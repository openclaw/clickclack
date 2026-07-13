package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestEventDeliveryQueueIsolatesWorkspacesAndSchedulesFairly(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := &eventDeliveryDispatcher{
		ctx:    ctx,
		cancel: cancel,
		queue:  make(chan eventDeliveryJob, eventDeliveryQueueSize),
	}
	dispatcher.startOnce.Do(func() {})

	for i := 0; i < eventDeliveryWorkspaceQueueSize; i++ {
		if !dispatcher.enqueue(eventDeliveryTestJob("workspace-a", i)) {
			t.Fatalf("workspace queue rejected job %d before capacity", i)
		}
	}
	if dispatcher.enqueue(eventDeliveryTestJob("workspace-a", eventDeliveryWorkspaceQueueSize)) {
		t.Fatal("workspace queue accepted work beyond its configured capacity")
	}
	if !dispatcher.enqueue(eventDeliveryTestJob("workspace-b", 0)) {
		t.Fatal("busy workspace starved another workspace")
	}

	first, ok := dispatcher.nextJob()
	if !ok || first.event.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected first job: %#v, ok=%v", first, ok)
	}
	second, ok := dispatcher.nextJob()
	if !ok || second.event.WorkspaceID != "workspace-b" {
		t.Fatalf("expected round-robin dispatch for workspace-b, got %#v, ok=%v", second, ok)
	}
	dispatcher.close()
}

func TestEventDeliveryCloseDrainsAndRecordsAttempts(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(callback.Close)
	st, serverState, ownerID, subscription, events := newEventDeliveryTestState(t, 5, localCallbackURL(callback.URL))
	t.Cleanup(serverState.Close)
	configureLocalCallbackPolicy(serverState)

	for _, event := range events {
		if !serverState.eventDeliveries.enqueue(eventDeliveryTestJobForSubscription(t, subscription, event)) {
			t.Fatal("event delivery queue rejected drain test job")
		}
	}
	for range eventDeliveryWorkerCount {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("event delivery workers did not start")
		}
	}

	closed := make(chan struct{})
	go func() {
		serverState.Close()
		close(closed)
	}()
	waitForEventDeliveryClose(t, serverState.eventDeliveries)
	if serverState.eventDeliveries.enqueue(eventDeliveryTestJob("workspace-late", 0)) {
		t.Fatal("dispatcher accepted work after close started")
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not drain promptly")
	}

	attempts, err := st.ListEventDeliveryAttempts(context.Background(), subscription.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != len(events) {
		t.Fatalf("expected %d recorded attempts, got %#v", len(events), attempts)
	}
	for _, attempt := range attempts {
		if attempt.ResponseStatus != http.StatusAccepted || attempt.Error != "" {
			t.Fatalf("expected successful terminal attempt, got %#v", attempt)
		}
	}
}

func TestEventDeliveryCloseAbortsOverdueCallbacksWithinDeadline(t *testing.T) {
	t.Parallel()
	st, serverState, ownerID, subscription, events := newEventDeliveryTestState(t, 5, "https://example.com/callback")
	callbackStarted := make(chan struct{}, eventDeliveryWorkerCount)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var callbackCount atomic.Int32
	serverState.callbackClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		callbackCount.Add(1)
		callbackStarted <- struct{}{}
		<-release
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})}
	t.Cleanup(serverState.Close)
	serverState.eventDeliveries.drainTimeout = 50 * time.Millisecond

	for _, event := range events {
		if !serverState.eventDeliveries.enqueue(eventDeliveryTestJobForSubscription(t, subscription, event)) {
			t.Fatal("event delivery queue rejected timeout test job")
		}
	}
	for range eventDeliveryWorkerCount {
		select {
		case <-callbackStarted:
		case <-time.After(time.Second):
			t.Fatal("event callback did not start")
		}
	}

	startedAt := time.Now()
	closed := make(chan struct{})
	go func() {
		serverState.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("dispatcher close exceeded its drain timeout")
	}
	if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
		t.Fatalf("dispatcher close exceeded its bound: %s", elapsed)
	}
	close(release)
	if got := callbackCount.Load(); got != eventDeliveryWorkerCount {
		t.Fatalf("expected queued callback to be abandoned after timeout, got %d callbacks", got)
	}
	time.Sleep(10 * time.Millisecond)
	attempts, err := st.ListEventDeliveryAttempts(context.Background(), subscription.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("shutdown deadline allowed post-close attempt writes: %#v", attempts)
	}
}

func TestEventDeliveryRevalidatesSubscriptionBeforeDispatch(t *testing.T) {
	t.Parallel()
	testEventDeliveryRevocation(t, false, func(
		ctx context.Context,
		st *sqlitestore.Store,
		ownerID string,
		subscription store.EventSubscription,
	) error {
		_, err := st.RevokeEventSubscription(ctx, subscription.ID, ownerID)
		return err
	})
}

func TestEventDeliveryRevalidatesAppInstallationBeforeDispatch(t *testing.T) {
	t.Parallel()
	testEventDeliveryRevocation(t, true, func(
		ctx context.Context,
		st *sqlitestore.Store,
		ownerID string,
		subscription store.EventSubscription,
	) error {
		_, err := st.RevokeAppInstallation(ctx, subscription.AppInstallationID, ownerID)
		return err
	})
}

func testEventDeliveryRevocation(
	t *testing.T,
	appBacked bool,
	revoke func(context.Context, *sqlitestore.Store, string, store.EventSubscription) error,
) {
	t.Helper()
	started := make(chan struct{}, eventDeliveryWorkerCount)
	release := make(chan struct{})
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(callback.Close)
	st, serverState, ownerID, subscription, events := newEventDeliveryTestState(t, 5, localCallbackURL(callback.URL))
	t.Cleanup(serverState.Close)
	configureLocalCallbackPolicy(serverState)

	if appBacked {
		bot, _, err := st.CreateBot(context.Background(), store.CreateBotInput{
			WorkspaceID: subscription.WorkspaceID,
			DisplayName: "Delivery App Bot",
			CreatedBy:   ownerID,
		})
		if err != nil {
			t.Fatal(err)
		}
		installation, err := st.CreateAppInstallation(context.Background(), store.CreateAppInstallationInput{
			WorkspaceID: subscription.WorkspaceID,
			AppSlug:     "delivery-app",
			DisplayName: "Delivery App",
			BotUserID:   bot.ID,
			CreatedBy:   ownerID,
		})
		if err != nil {
			t.Fatal(err)
		}
		subscription, err = st.CreateEventSubscription(context.Background(), store.CreateEventSubscriptionInput{
			WorkspaceID:       subscription.WorkspaceID,
			AppInstallationID: installation.ID,
			EventTypes:        []string{"message.created"},
			CallbackURL:       localCallbackURL(callback.URL),
			CreatedBy:         ownerID,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, event := range events[:eventDeliveryWorkerCount] {
		if !serverState.eventDeliveries.enqueue(eventDeliveryTestJobForSubscription(t, subscription, event)) {
			t.Fatal("event delivery queue rejected blocker job")
		}
	}
	for range eventDeliveryWorkerCount {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("event delivery workers did not start")
		}
	}
	if !serverState.eventDeliveries.enqueue(eventDeliveryTestJobForSubscription(t, subscription, events[eventDeliveryWorkerCount])) {
		t.Fatal("event delivery queue rejected queued revocation job")
	}
	if err := revoke(context.Background(), st, ownerID, subscription); err != nil {
		t.Fatal(err)
	}
	close(release)
	serverState.Close()

	select {
	case <-started:
		t.Fatal("queued event callback ran after revocation")
	default:
	}
	attempts, err := st.ListEventDeliveryAttempts(context.Background(), subscription.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != len(events) {
		t.Fatalf("expected %d terminal attempts, got %#v", len(events), attempts)
	}
	inactiveAttempts := 0
	for _, attempt := range attempts {
		if attempt.Error == errEventDeliverySubscriptionInactive.Error() {
			inactiveAttempts++
		}
	}
	if inactiveAttempts != 1 {
		t.Fatalf("expected one inactive-subscription attempt, got %#v", attempts)
	}
}

func newEventDeliveryTestState(
	t *testing.T,
	eventCount int,
	callbackURL string,
) (*sqlitestore.Store, *Server, string, store.EventSubscription, []store.Event) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "event-delivery@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID: workspaces[0].ID,
		EventTypes:  []string{"message.created"},
		CallbackURL: callbackURL,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := make([]store.Event, 0, eventCount)
	for i := 0; i < eventCount; i++ {
		_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channels[0].ID,
			AuthorID:  owner.ID,
			Body:      "delivery test",
			Nonce:     strings.Repeat("x", i+1),
		})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	serverState := New(st, realtime.NewHub(), Options{})
	return st, serverState, owner.ID, subscription, events
}

func eventDeliveryTestJob(workspaceID string, index int) eventDeliveryJob {
	return eventDeliveryJob{
		workspaceID: workspaceID,
		event: store.Event{
			ID:          workspaceID + "-event-" + strconv.Itoa(index),
			WorkspaceID: workspaceID,
		},
	}
}

func waitForEventDeliveryClose(t *testing.T, dispatcher *eventDeliveryDispatcher) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		dispatcher.mu.Lock()
		closing := dispatcher.closing
		dispatcher.mu.Unlock()
		if closing {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("dispatcher close did not start")
}

func eventDeliveryTestJobForSubscription(t *testing.T, subscription store.EventSubscription, event store.Event) eventDeliveryJob {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"subscription_id": subscription.ID,
		"event":           event,
	})
	if err != nil {
		t.Fatal(err)
	}
	return eventDeliveryJob{
		subscriptionID: subscription.ID,
		workspaceID:    subscription.WorkspaceID,
		event:          event,
		payload:        payload,
	}
}
