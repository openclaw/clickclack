package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
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
	st, serverState, ownerID, subscription, events := newEventDeliveryTestState(t, 5)

	started := make(chan struct{}, len(events))
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
	t.Cleanup(serverState.Close)
	configureLocalCallbackPolicy(serverState)
	subscription.CallbackURL = localCallbackURL(callback.URL)

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

func TestEventDeliveryCloseBoundsOverdueCallbacksAndRecordsAttempts(t *testing.T) {
	t.Parallel()
	st, serverState, ownerID, subscription, events := newEventDeliveryTestState(t, 1)
	callbackStarted := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	callback := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(callbackStarted)
		<-release
	}))
	t.Cleanup(callback.Close)
	t.Cleanup(serverState.Close)
	configureLocalCallbackPolicy(serverState)
	subscription.CallbackURL = localCallbackURL(callback.URL)
	serverState.eventDeliveries.drainTimeout = 50 * time.Millisecond

	if !serverState.eventDeliveries.enqueue(eventDeliveryTestJobForSubscription(t, subscription, events[0])) {
		t.Fatal("event delivery queue rejected timeout test job")
	}
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("event callback did not start")
	}

	startedAt := time.Now()
	serverState.Close()
	close(release)
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("dispatcher close exceeded its bound: %s", elapsed)
	}
	attempts, err := st.ListEventDeliveryAttempts(context.Background(), subscription.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Error == "" {
		t.Fatalf("expected one terminal cancellation attempt, got %#v", attempts)
	}
}

func newEventDeliveryTestState(
	t *testing.T,
	eventCount int,
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
		CallbackURL: "https://example.com/callback",
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
		subscription: store.EventSubscription{WorkspaceID: workspaceID},
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
	return eventDeliveryJob{subscription: subscription, event: event, payload: payload}
}
