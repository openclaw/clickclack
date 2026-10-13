package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
	"github.com/openclaw/clickclack/apps/api/internal/webpush"
)

type recordedPushResult struct {
	kind       string
	userID     string
	endpoint   string
	retryAfter time.Duration
}

type fakeSubscriptionStore struct {
	mu            sync.Mutex
	results       []recordedPushResult
	err           error
	lookupErr     error
	message       store.Message
	messageErr    error
	preference    string
	preferenceErr error
	mentionErr    error
	prunes        int
	prunePanic    bool
	pruneErr      error
}

// GetPushSubscriptionDelivery answers the device the notification named, with
// the RFC 8291 example keys, unless the test says the store refuses it.
func (f *fakeSubscriptionStore) GetPushSubscriptionDelivery(_ context.Context, _, endpoint, _ string) (store.PushSubscriptionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lookupErr != nil {
		return store.PushSubscriptionTarget{}, f.lookupErr
	}
	return store.PushSubscriptionTarget{Endpoint: endpoint, P256dh: exampleClientKey, Auth: exampleClientAuth}, nil
}

func (f *fakeSubscriptionStore) GetMessage(context.Context, string, string) (store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.message, f.messageErr
}

// GetChannelNotificationPreference answers the preference the test set, or
// the default a user with no setting has.
func (f *fakeSubscriptionStore) GetChannelNotificationPreference(context.Context, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.preference == "" {
		return store.ChannelNotifyAll, f.preferenceErr
	}
	return f.preference, f.preferenceErr
}

// ListMentionedUserIDs mentions nobody, unless the test says the lookup fails.
func (f *fakeSubscriptionStore) ListMentionedUserIDs(context.Context, string, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return nil, f.mentionErr
}

func (f *fakeSubscriptionStore) DeletePushSubscription(_ context.Context, userID, endpoint string) error {
	return f.record("delete", userID, endpoint, 0)
}

func (f *fakeSubscriptionStore) MarkPushSubscriptionSuccess(_ context.Context, userID, endpoint string) error {
	return f.record("success", userID, endpoint, 0)
}

func (f *fakeSubscriptionStore) MarkPushSubscriptionFailure(_ context.Context, userID, endpoint string, retryAfter time.Duration) (int64, error) {
	return 1, f.record("failure", userID, endpoint, retryAfter)
}

// PrunePushSubscriptions counts each sweep, and fails or panics when the test
// says the store does.
func (f *fakeSubscriptionStore) PrunePushSubscriptions(context.Context, string, time.Time) (store.PushPruneResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prunes++
	if f.prunePanic {
		panic("prune exploded")
	}
	return store.PushPruneResult{}, f.pruneErr
}

func (f *fakeSubscriptionStore) pruneCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prunes
}

func (f *fakeSubscriptionStore) record(kind, userID, endpoint string, retryAfter time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, recordedPushResult{kind: kind, userID: userID, endpoint: endpoint, retryAfter: retryAfter})
	return f.err
}

func (f *fakeSubscriptionStore) recorded() []recordedPushResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedPushResult(nil), f.results...)
}

type fakeSender struct {
	mu            sync.Mutex
	err           error
	sent          int
	hold          chan struct{}
	delay         time.Duration
	panc          bool
	subscriptions []webpush.Subscription
}

func (f *fakeSender) Send(_ context.Context, subscription webpush.Subscription, _ webpush.Message) error {
	f.mu.Lock()
	hold := f.hold
	delay := f.delay
	err := f.err
	shouldPanic := f.panc
	f.sent++
	f.subscriptions = append(f.subscriptions, subscription)
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if shouldPanic {
		panic("push exploded")
	}
	return err
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent
}

func pushNotificationFor(userID, endpoint string) PushNotification {
	return PushNotification{
		UserID:    userID,
		MessageID: "msg_1",
		Title:     "Owner in #general",
		Tag:       "clickclack:msg_1",
		URL:       "/app/wsp_1/chn_1",
		Subscriptions: []store.PushSubscriptionTarget{{
			Endpoint: endpoint,
			P256dh:   exampleClientKey,
			Auth:     exampleClientAuth,
		}},
	}
}

func TestWebPushNotifierRecordsWhatTheRelaySaid(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		err        error
		want       string
		retryAfter time.Duration
	}{
		"delivered":    {nil, "success", 0},
		"gone":         {webpush.ErrSubscriptionGone, "delete", 0},
		"rate limited": {&webpush.RelayError{Host: "push.example.com", Status: http.StatusTooManyRequests, RetryAfter: time.Minute}, "failure", time.Minute},
		"unavailable":  {&webpush.RelayError{Host: "push.example.com", Status: http.StatusServiceUnavailable}, "failure", 0},
		"transport":    {errors.New("network down"), "failure", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subscriptions := &fakeSubscriptionStore{}
			notifier := newWebPushNotifier(&fakeSender{err: testCase.err}, subscriptions, "")
			if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", "https://push.example.com/send/device")); err != nil {
				t.Fatal(err)
			}
			notifier.Close()
			recorded := subscriptions.recorded()
			if len(recorded) != 1 || recorded[0].kind != testCase.want {
				t.Fatalf("recorded %#v, want %s", recorded, testCase.want)
			}
			if recorded[0].userID != "usr_1" || recorded[0].endpoint != "https://push.example.com/send/device" {
				t.Fatalf("unexpected bookkeeping target %#v", recorded[0])
			}
			if recorded[0].retryAfter != testCase.retryAfter {
				t.Fatalf("retry after %s, want %s", recorded[0].retryAfter, testCase.retryAfter)
			}
		})
	}
}

func TestWebPushNotifierToleratesBookkeepingFailures(t *testing.T) {
	t.Parallel()
	subscriptions := &fakeSubscriptionStore{err: errors.New("database is away")}
	notifier := newWebPushNotifier(&fakeSender{}, subscriptions, "")
	if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", "https://push.example.com/send/device")); err != nil {
		t.Fatal(err)
	}
	notifier.Notify(context.Background(), PushNotification{UserID: "usr_2"})
	notifier.Close()
	if len(subscriptions.recorded()) != 1 {
		t.Fatalf("unexpected bookkeeping %#v", subscriptions.recorded())
	}
}

// TestWebPushNotifierSurvivesAPanickingDelivery guards the detached pool: an
// unrecovered panic in a worker would take the whole server down. One
// endpoint keeps a single in-flight send. Later pushes for it wait, and
// nothing accepted may disappear.
func TestWebPushNotifierSurvivesAPanickingDelivery(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{panc: true}
	subscriptions := &fakeSubscriptionStore{}
	notifier := newWebPushNotifier(sender, subscriptions, "")
	const burst = 8
	for range burst {
		if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", "https://push.example.com/send/device")); err != nil {
			t.Fatal(err)
		}
	}
	notifier.Close()
	if attempts := sender.count(); attempts != burst {
		t.Fatalf("every accepted delivery must be attempted after a panic, got %d", attempts)
	}
	if len(subscriptions.recorded()) != 0 {
		t.Fatalf("a panicking delivery records nothing: %#v", subscriptions.recorded())
	}
}

func TestWebPushNotifierDropsWhenTheQueueIsFull(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	sender := &fakeSender{hold: hold}
	notifier := newWebPushNotifier(sender, &fakeSubscriptionStore{}, "")
	notification := pushNotificationFor("usr_1", "https://push.example.com/send/device")
	for range webPushQueueSize*2 + webPushWorkers + 50 {
		if err := notifier.Notify(context.Background(), notification); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	var dropped int64
	for time.Now().Before(deadline) {
		notifier.dropMu.Lock()
		dropped = notifier.dropped
		notifier.dropMu.Unlock()
		if dropped > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if dropped == 0 {
		t.Fatal("expected the overflow to be counted")
	}
	notifier.flightMu.Lock()
	waiters := 0
	for _, list := range notifier.pending {
		waiters += len(list)
	}
	pending := notifier.pendingCount
	notifier.flightMu.Unlock()
	queued := len(notifier.queue)
	t.Logf("pending=%d waiters=%d channel=%d dropped=%d", pending, waiters, queued, dropped)
	if pending > webPushQueueSize || waiters+queued > webPushQueueSize {
		t.Fatalf("pending budget exceeded: pending=%d waiters=%d channel=%d", pending, waiters, queued)
	}
	close(hold)
	notifier.Close()
}

func TestWebPushNotifierRefusesAfterClose(t *testing.T) {
	t.Parallel()
	notifier := newWebPushNotifier(&fakeSender{}, &fakeSubscriptionStore{}, "")
	notifier.Close()
	notifier.Close()
	if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", "https://push.example.com/send/device")); err == nil {
		t.Fatal("expected a closed notifier to refuse work")
	}
}

// TestWebPushNotifierKeepsEndpointsOutOfTheLog is the redaction guard at the
// layer that writes the log line: a timeout arrives as a *url.Error whose text
// is the full endpoint, and the endpoint path is a delivery secret.
func TestWebPushNotifierKeepsEndpointsOutOfTheLog(t *testing.T) {
	release := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer func() {
		close(release)
		relay.Close()
	}()
	publicKey, privateKey, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	client := relay.Client()
	client.Timeout = 100 * time.Millisecond
	notifier := newWebPushNotifier(&webpush.Sender{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    "https://chat.example.com",
		Client:     client,
	}, &fakeSubscriptionStore{}, "")

	var captured strings.Builder
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&captured)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	}()

	endpoint := relay.URL + "/send/device-delivery-secret"
	if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", endpoint)); err != nil {
		t.Fatal(err)
	}
	notifier.Close()
	logged := captured.String()
	if logged == "" {
		t.Fatal("expected the failure to be logged")
	}
	if strings.Contains(logged, "device-delivery-secret") || strings.Contains(logged, endpoint) {
		t.Fatalf("the log leaked the endpoint: %s", logged)
	}
}

// TestWebPushNotifierBacksOffATimedOutRelay sends to a relay that never
// answers, so the send budget is spent before the failure is recorded. The
// failure and its backoff must still reach the store. Not parallel: it
// shortens the package send timeout.
func TestWebPushNotifierBacksOffATimedOutRelay(t *testing.T) {
	release := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer func() {
		close(release)
		relay.Close()
	}()
	previousTimeout := webPushSendTimeout
	webPushSendTimeout = 200 * time.Millisecond
	defer func() { webPushSendTimeout = previousTimeout }()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open("sqlite://" + databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "timeout-owner@example.com")
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
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "into the void"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := relay.URL + "/send/device"
	if _, err := st.UpsertPushSubscription(ctx, store.PushSubscriptionInput{
		UserID: owner.ID, Endpoint: endpoint, P256dh: exampleClientKey, Auth: exampleClientAuth, SessionToken: session.Token,
	}); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	notifier := newWebPushNotifier(&webpush.Sender{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    "https://chat.example.com",
		Client:     relay.Client(),
	}, st, "")
	if err := notifier.Notify(ctx, PushNotification{
		UserID:        owner.ID,
		MessageID:     message.ID,
		Subscriptions: []store.PushSubscriptionTarget{{Endpoint: endpoint}},
	}); err != nil {
		t.Fatal(err)
	}
	notifier.Close()

	db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var failures int64
	var nextAttempt sql.NullString
	if err := db.QueryRow(
		`SELECT failure_count, next_attempt_at FROM user_push_subscriptions WHERE endpoint = ?`, endpoint,
	).Scan(&failures, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	if failures != 1 {
		t.Fatalf("the timed-out delivery recorded %d failures, want 1", failures)
	}
	if !nextAttempt.Valid || nextAttempt.String == "" {
		t.Fatal("the timed-out device has no backoff, so every message would retry it")
	}
}

// TestWebPushNotifierCapsInFlightDeliveriesPerSubscription holds one endpoint
// inside Send and queues more pushes for it. Only one worker may stay there.
// A different endpoint must still get a worker from the rest of the pool.
func TestWebPushNotifierCapsInFlightDeliveriesPerSubscription(t *testing.T) {
	t.Parallel()
	sender := &concurrencySender{started: make(chan string, 16), hold: make(chan struct{})}
	notifier := newWebPushNotifier(sender, &fakeSubscriptionStore{}, "")
	t.Cleanup(notifier.Close)
	t.Cleanup(sender.release)

	slow := "https://push.example.com/send/slow"
	for range 8 {
		if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", slow)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case got := <-sender.started:
		if got != slow {
			t.Fatalf("first send was %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery started")
	}
	time.Sleep(50 * time.Millisecond)
	if max := sender.maxConcurrent(slow); max != 1 {
		t.Fatalf("subscription had %d deliveries in flight, want 1", max)
	}

	other := "https://push.example.com/send/other"
	if err := notifier.Notify(context.Background(), pushNotificationFor("usr_2", other)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sender.maxConcurrent(other) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("a different subscription never started while the slow one was in flight")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if max := sender.maxConcurrent(slow); max != 1 {
		t.Fatalf("subscription had %d deliveries in flight after another device started, want 1", max)
	}
}

// TestWebPushNotifierIsolatesEndpointsOverHTTP holds one real encrypted send
// and lets a second endpoint proceed. Extra pushes for the held endpoint do
// not take more workers.
func TestWebPushNotifierIsolatesEndpointsOverHTTP(t *testing.T) {
	ctx := context.Background()
	st, owner, channel, _ := newWebPushTestStore(t)
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "burst"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	slowInFlight, slowMax, slowRequests, otherStarted := 0, 0, 0, 0
	hold := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/slow") {
			mu.Lock()
			slowInFlight++
			slowRequests++
			if slowInFlight > slowMax {
				slowMax = slowInFlight
			}
			mu.Unlock()
			<-hold
			mu.Lock()
			slowInFlight--
			mu.Unlock()
		} else {
			mu.Lock()
			otherStarted++
			mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
	}))
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(hold) }) }
	t.Cleanup(func() {
		release()
		relay.Close()
	})
	slow := relay.URL + "/slow"
	other := relay.URL + "/other"
	registerKeyedDevice(t, st, owner.ID, slow, "")
	registerKeyedDevice(t, st, owner.ID, other, "")
	publicKey, privateKey, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	notifier := newWebPushNotifier(&webpush.Sender{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    "https://chat.example.com",
		Client:     relay.Client(),
	}, st, "")
	t.Cleanup(notifier.Close)
	t.Cleanup(release)
	notify := func(endpoint string) {
		t.Helper()
		if err := notifier.Notify(ctx, PushNotification{
			UserID:        owner.ID,
			MessageID:     message.ID,
			Subscriptions: []store.PushSubscriptionTarget{{Endpoint: endpoint}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	notify(slow)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		started := slowInFlight == 1
		mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the slow endpoint never accepted a request")
		}
		time.Sleep(10 * time.Millisecond)
	}
	notify(slow)
	notify(slow)
	notify(slow)
	notify(other)
	deadline = time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		ready := otherStarted == 1 && slowMax == 1
		mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			other, max := otherStarted, slowMax
			mu.Unlock()
			t.Fatalf("other=%d slow_max=%d", other, max)
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	deadline = time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := slowRequests == 4 && slowInFlight == 0
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			requests, inFlight := slowRequests, slowInFlight
			mu.Unlock()
			t.Fatalf("slow_requests=%d in_flight=%d", requests, inFlight)
		}
		time.Sleep(10 * time.Millisecond)
	}
	notifier.Close()
	mu.Lock()
	t.Logf("slow_max=%d slow_requests=%d other_while_held=%d", slowMax, slowRequests, otherStarted)
	mu.Unlock()
}

type concurrencySender struct {
	mu      sync.Mutex
	current map[string]int
	max     map[string]int
	started chan string
	hold    chan struct{}
	once    sync.Once
}

func (c *concurrencySender) Send(_ context.Context, subscription webpush.Subscription, _ webpush.Message) error {
	endpoint := subscription.Endpoint
	c.mu.Lock()
	if c.current == nil {
		c.current = map[string]int{}
		c.max = map[string]int{}
	}
	c.current[endpoint]++
	if c.current[endpoint] > c.max[endpoint] {
		c.max[endpoint] = c.current[endpoint]
	}
	c.mu.Unlock()
	c.started <- endpoint
	<-c.hold
	c.mu.Lock()
	c.current[endpoint]--
	c.mu.Unlock()
	return nil
}

func (c *concurrencySender) maxConcurrent(endpoint string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max[endpoint]
}

func (c *concurrencySender) release() {
	c.once.Do(func() { close(c.hold) })
}

// TestWebPushNotifierRecordsASlowSuccess records a delayed 201 as a normal success. It does not start a cooldown.
func TestWebPushNotifierRecordsASlowSuccess(t *testing.T) {
	previousTimeout := webPushSendTimeout
	webPushSendTimeout = 2 * time.Second
	defer func() { webPushSendTimeout = previousTimeout }()

	subscriptions := &fakeSubscriptionStore{}
	notifier := newWebPushNotifier(&fakeSender{delay: 180 * time.Millisecond}, subscriptions, "")
	if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", "https://push.example.com/send/device")); err != nil {
		t.Fatal(err)
	}
	notifier.Close()
	recorded := subscriptions.recorded()
	if len(recorded) != 1 || recorded[0].kind != "success" {
		t.Fatalf("slow success bookkeeping = %#v", recorded)
	}
	if recorded[0].userID != "usr_1" || recorded[0].endpoint != "https://push.example.com/send/device" {
		t.Fatalf("unexpected bookkeeping target %#v", recorded[0])
	}
}

// TestSlowSuccessfulPushLeavesFailureHistoryClear sends through webpush.Sender
// to a local relay that waits and then returns 201. The sqlite row records
// success without a cooldown or a failure run.
func TestSlowSuccessfulPushLeavesFailureHistoryClear(t *testing.T) {
	previousTimeout := webPushSendTimeout
	webPushSendTimeout = 2 * time.Second
	defer func() { webPushSendTimeout = previousTimeout }()

	ctx := context.Background()
	st, owner, channel, databasePath := newWebPushTestStore(t)
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "slow but delivered"})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(180 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(relay.Close)
	endpoint := relay.URL + "/send/device"
	registerKeyedDevice(t, st, owner.ID, endpoint, "")
	publicKey, privateKey, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	notifier := newWebPushNotifier(&webpush.Sender{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    "https://chat.example.com",
		Client:     relay.Client(),
	}, st, "")
	if err := notifier.Notify(ctx, PushNotification{
		UserID:        owner.ID,
		MessageID:     message.ID,
		Subscriptions: []store.PushSubscriptionTarget{{Endpoint: endpoint}},
	}); err != nil {
		t.Fatal(err)
	}
	notifier.Close()

	db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var failures int64
	var failingSince, lastSuccess, nextAttempt sql.NullString
	if err := db.QueryRow(
		`SELECT failure_count, failing_since, last_success_at, next_attempt_at FROM user_push_subscriptions WHERE endpoint = ?`,
		endpoint,
	).Scan(&failures, &failingSince, &lastSuccess, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	t.Logf("failure_count=%d failing_since_set=%t last_success_set=%t next_attempt_set=%t", failures, failingSince.Valid, lastSuccess.Valid, nextAttempt.Valid)
	if failures != 0 || failingSince.Valid || !lastSuccess.Valid || nextAttempt.Valid {
		t.Fatalf("row failure_count=%d failing_since=%q last_success=%q next_attempt=%q", failures, failingSince.String, lastSuccess.String, nextAttempt.String)
	}
}

// TestWebPushNotifierStoresTheRelaysRetryAfter follows a refusal from the push
// service to the backoff on the stored row. Retry-After may be a number of
// seconds or an HTTP-date, and either way the device is not tried again before
// the relay asked; a date already past or a value that is neither leaves the
// ladder in charge, and no answer schedules a device past the cap.
func TestWebPushNotifierStoresTheRelaysRetryAfter(t *testing.T) {
	t.Parallel()
	firstRung := time.Minute
	for name, testCase := range map[string]struct {
		header   func(now time.Time) string
		earliest func(before, asked time.Time) time.Time
		latest   func(after, asked time.Time) time.Time
	}{
		"seconds": {
			header:   func(time.Time) string { return "120" },
			earliest: func(before, _ time.Time) time.Time { return before.Add(2 * time.Minute) },
			latest:   func(after, _ time.Time) time.Time { return after.Add(2 * time.Minute) },
		},
		"http date": {
			header:   func(now time.Time) string { return now.Add(10 * time.Minute).UTC().Format(http.TimeFormat) },
			earliest: func(_, asked time.Time) time.Time { return asked },
			latest:   func(after, _ time.Time) time.Time { return after.Add(10 * time.Minute) },
		},
		"http date already past": {
			header:   func(now time.Time) string { return now.Add(-time.Hour).UTC().Format(http.TimeFormat) },
			earliest: func(before, _ time.Time) time.Time { return before.Add(firstRung) },
			latest:   func(after, _ time.Time) time.Time { return after.Add(firstRung) },
		},
		"neither form": {
			header:   func(time.Time) string { return "soon" },
			earliest: func(before, _ time.Time) time.Time { return before.Add(firstRung) },
			latest:   func(after, _ time.Time) time.Time { return after.Add(firstRung) },
		},
		"http date past the cap": {
			header:   func(now time.Time) string { return now.AddDate(1, 0, 0).UTC().Format(http.TimeFormat) },
			earliest: func(before, _ time.Time) time.Time { return before.Add(store.MaxPushRetryDelay) },
			latest:   func(after, _ time.Time) time.Time { return after.Add(store.MaxPushRetryDelay) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st, owner, channel, databasePath := newWebPushTestStore(t)
			message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "slow down"})
			if err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			header := testCase.header(before)
			asked, _ := http.ParseTime(header)
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", header)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(relay.Close)
			endpoint := relay.URL + "/send/device"
			registerKeyedDevice(t, st, owner.ID, endpoint, "")
			publicKey, privateKey, err := webpush.GenerateKeys()
			if err != nil {
				t.Fatal(err)
			}
			notifier := newWebPushNotifier(&webpush.Sender{
				PublicKey:  publicKey,
				PrivateKey: privateKey,
				Subject:    "https://chat.example.com",
				Client:     relay.Client(),
			}, st, "")
			if err := notifier.Notify(ctx, PushNotification{
				UserID:        owner.ID,
				MessageID:     message.ID,
				Subscriptions: []store.PushSubscriptionTarget{{Endpoint: endpoint}},
			}); err != nil {
				t.Fatal(err)
			}
			notifier.Close()
			after := time.Now()

			db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var failures int64
			var stored sql.NullString
			if err := db.QueryRow(
				`SELECT failure_count, next_attempt_at FROM user_push_subscriptions WHERE endpoint = ?`, endpoint,
			).Scan(&failures, &stored); err != nil {
				t.Fatal(err)
			}
			if failures != 1 {
				t.Fatalf("the refused delivery recorded %d failures, want 1", failures)
			}
			next, err := time.Parse(time.RFC3339Nano, stored.String)
			if err != nil {
				t.Fatalf("next_attempt_at %q: %v", stored.String, err)
			}
			if earliest := testCase.earliest(before, asked); next.Before(earliest) {
				t.Fatalf("Retry-After %q stored next_attempt_at %s, before %s", header, next.UTC().Format(time.RFC3339), earliest.UTC().Format(time.RFC3339))
			}
			if latest := testCase.latest(after, asked); next.After(latest) {
				t.Fatalf("Retry-After %q stored next_attempt_at %s, after %s", header, next.UTC().Format(time.RFC3339), latest.UTC().Format(time.RFC3339))
			}
		})
	}
}

// TestMessageNotificationsFanOutPerChannel proves the two delivery paths stay
// separate: a Pushover-only user produces exactly one Pushover call and no web
// push, and a push-only user the reverse.
func TestMessageNotificationsFanOutPerChannel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(dataDir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "fanout-owner@example.com")
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
	pushoverUser, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Pushover", Email: "fanout-pushover@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	pushUser, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Push", Email: "fanout-push@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []store.User{pushoverUser, pushUser} {
		if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, user.ID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: pushoverUser.ID,
		NotificationSettings: &store.NotificationSettings{
			PushoverEnabled: true,
			PushoverUserKey: "u12345678901234567890123456789",
		},
	}); err != nil {
		t.Fatal(err)
	}
	pushSession, err := st.CreateSession(ctx, pushUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, store.PushSubscriptionInput{
		UserID:       pushUser.ID,
		Endpoint:     "https://push.example.com/send/fanout",
		P256dh:       exampleClientKey,
		Auth:         exampleClientAuth,
		UserAgent:    "phone",
		SessionToken: pushSession.Token,
	}); err != nil {
		t.Fatal(err)
	}

	pushover := &recordingNotifier{}
	web := &recordingNotifier{}
	publicKey, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		PushNotifier:     pushover,
		WebPushNotifier:  web,
		WebPushPublicKey: publicKey,
	}).Handler())
	t.Cleanup(server.Close)

	posted := postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channels[0].ID+"/messages", map[string]any{"body": "morning"})

	if len(pushover.notifications) != 1 {
		t.Fatalf("expected one Pushover call, got %#v", pushover.notifications)
	}
	if pushover.notifications[0].RecipientKey == "" || pushover.notifications[0].Title != "ClickClack" {
		t.Fatalf("the Pushover payload changed: %#v", pushover.notifications[0])
	}
	if len(pushover.notifications[0].Subscriptions) != 0 {
		t.Fatal("Pushover must not receive web push devices")
	}
	if len(web.notifications) != 1 {
		t.Fatalf("expected one web push call, got %#v", web.notifications)
	}
	delivered := web.notifications[0]
	if delivered.UserID != pushUser.ID || len(delivered.Subscriptions) != 1 {
		t.Fatalf("unexpected web push recipient: %#v", delivered)
	}
	if delivered.Title != "Owner in #general" {
		t.Fatalf("unexpected title %q", delivered.Title)
	}
	// The queue holds no text: the worker reads it from the message when it
	// sends, which TestQueuedWebPushCarriesTheTextAtSendTime checks in what the
	// phone decrypts.
	if delivered.Message != "" {
		t.Fatalf("the queued web push carries text %q", delivered.Message)
	}
	if delivered.Tag != "clickclack:"+posted.Message.ID {
		t.Fatalf("unexpected tag %q", delivered.Tag)
	}
	if !strings.HasPrefix(delivered.URL, "/app/"+workspaces[0].ID+"/") {
		t.Fatalf("unexpected route %q", delivered.URL)
	}
	if delivered.RecipientKey != "" {
		t.Fatal("a web push recipient has no Pushover key")
	}
}

// The tap URL pairs the workspace's storage id with a target, so the target
// must be a storage id too: the route API reads a storage workspace id as a
// legacy pair and resolves only storage targets under it. A channel's route id
// there answers 404 and the app falls back to the workspace's default channel.
func TestWebPushTapURLResolvesToTheChannel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "tap-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "elsewhere", Kind: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if channel.RouteID == "" {
		t.Fatal("the channel needs a route id for this test to mean anything")
	}
	recipient, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Recipient", Email: "tap-recipient@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, recipient.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(ctx, recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, store.PushSubscriptionInput{
		UserID:       recipient.ID,
		Endpoint:     "https://push.example.com/send/tap",
		P256dh:       exampleClientKey,
		Auth:         exampleClientAuth,
		UserAgent:    "phone",
		SessionToken: session.Token,
	}); err != nil {
		t.Fatal(err)
	}
	web := &recordingNotifier{}
	publicKey, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		WebPushNotifier:  web,
		WebPushPublicKey: publicKey,
	}).Handler())
	t.Cleanup(server.Close)

	postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]any{"body": "over here"})
	if len(web.notifications) != 1 {
		t.Fatalf("expected one web push call, got %#v", web.notifications)
	}
	tap := web.notifications[0].URL
	if tap != "/app/"+workspace.ID+"/"+channel.ID {
		t.Fatalf("a channel tap must pair storage ids, got %q", tap)
	}
	if strings.Contains(tap, channel.RouteID) {
		t.Fatalf("a channel tap under a storage workspace id must not carry the route id %q: %q", channel.RouteID, tap)
	}
	resolved := getJSONAsUser[struct {
		Route store.RouteTarget `json:"route"`
	}](t, recipient.ID, server.URL+"/api/routes"+strings.TrimPrefix(tap, "/app"))
	if resolved.Route.TargetType != "channel" || resolved.Route.TargetID != channel.ID ||
		resolved.Route.CanonicalPath != "/app/"+workspace.RouteID+"/"+channel.RouteID {
		t.Fatalf("the tap must resolve to the channel it names: %#v", resolved.Route)
	}
}

func TestWebPushTitleNamesThePlaceItKnows(t *testing.T) {
	t.Parallel()
	author := store.Message{AuthorID: "usr_1", Author: &store.User{DisplayName: "Ari"}}
	displayTitle := "  Release Room  "
	for name, testCase := range map[string]struct {
		message store.Message
		place   store.Channel
		want    string
	}{
		"channel":       {author, store.Channel{Name: "general"}, "Ari in #general"},
		"display title": {author, store.Channel{Name: "general", DisplayTitle: &displayTitle}, "Ari in #Release Room"},
		"direct message": {
			store.Message{AuthorID: "usr_1", Author: &store.User{DisplayName: "Ari"}, DirectConversationID: "dm_1"},
			store.Channel{},
			"Ari in Direct message",
		},
		"blank author": {
			store.Message{AuthorID: "usr_1", Author: &store.User{DisplayName: "  "}},
			store.Channel{Name: "general"},
			"ClickClack in #general",
		},
		"unreadable channel": {store.Message{AuthorID: "usr_1"}, store.Channel{}, "ClickClack"},
	} {
		if got := webPushTitle(testCase.message, testCase.place); got != testCase.want {
			t.Fatalf("%s: title is %q, want %q", name, got, testCase.want)
		}
	}
}

func TestWebPushBodyAndRouteDescribeTheMessage(t *testing.T) {
	t.Parallel()
	parent := "msg_root"
	if got := webPushBody(store.Message{Body: "  hello  "}); got != "hello" {
		t.Fatalf("body is %q", got)
	}
	if got := webPushBody(store.Message{}); got != "New message" {
		t.Fatalf("empty body is %q", got)
	}
	// The workspace segment is a storage id, so the target is one too; the
	// route API canonicalizes the pair.
	channel := store.Message{WorkspaceID: "wsp_1", ChannelID: "chn_1"}
	if got := webPushURL(channel); got != "/app/wsp_1/chn_1" {
		t.Fatalf("channel route is %q", got)
	}
	thread := store.Message{WorkspaceID: "wsp_1", ChannelID: "chn_1", ParentMessageID: &parent, ThreadRootID: parent}
	if got := webPushURL(thread); got != "/app/wsp_1/chn_1" {
		t.Fatalf("thread route is %q", got)
	}
	dm := store.Message{WorkspaceID: "wsp_1", DirectConversationID: "dm_1"}
	if got := webPushURL(dm); got != "/app/wsp_1/dm_1" {
		t.Fatalf("direct route is %q", got)
	}
	if got := webPushURL(store.Message{}); got != "/app" {
		t.Fatalf("unknown route is %q", got)
	}
	if got := webPushTag(store.Message{ID: "msg_1"}); got != "clickclack:msg_1" {
		t.Fatalf("tag is %q", got)
	}
}

// TestWebPushNotifierRevalidatesBeforeSending covers every reason a queued push
// is refused at send time. None of them may reach the push service, and each
// names its reason in one log line.
func TestWebPushNotifierRevalidatesBeforeSending(t *testing.T) {
	for name, testCase := range map[string]struct {
		lookupErr     error
		messageErr    error
		messageID     string
		preference    string
		preferenceErr error
		mentionErr    error
		reason        string
	}{
		"device removed":       {lookupErr: sql.ErrNoRows, messageID: "msg_1", reason: "the device is no longer registered"},
		"session ended":        {lookupErr: store.ErrPushSessionEnded, messageID: "msg_1", reason: "the session that registered the device has ended"},
		"backing off":          {lookupErr: store.ErrPushSubscriptionBackingOff, messageID: "msg_1", reason: "the device is backing off"},
		"key retired":          {lookupErr: store.ErrPushSubscriptionKeyRetired, messageID: "msg_1", reason: "the device was registered under a retired key"},
		"device lookup failed": {lookupErr: errors.New("database is away"), messageID: "msg_1", reason: "the device could not be verified"},
		"message unreadable":   {messageErr: sql.ErrNoRows, messageID: "msg_1", reason: "the user can no longer read the message"},
		"message lookup failed": {
			messageErr: errors.New("database is away"), messageID: "msg_1", reason: "the message could not be verified",
		},
		"no message":    {reason: "the message could not be verified"},
		"channel muted": {messageID: "msg_1", preference: store.ChannelNotifyMuted, reason: "the recipient muted the channel"},
		"not mentioned": {messageID: "msg_1", preference: store.ChannelNotifyMentions, reason: "the message does not mention the recipient"},
		"preference lookup failed": {
			messageID: "msg_1", preferenceErr: errors.New("database is away"), reason: "the recipient's notification preference could not be verified",
		},
		"mention lookup failed": {
			messageID: "msg_1", mentionErr: errors.New("database is away"), reason: "the message's mentions could not be verified",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var captured strings.Builder
			previousOutput := log.Writer()
			log.SetOutput(&captured)
			defer log.SetOutput(previousOutput)

			sender := &fakeSender{}
			subscriptions := &fakeSubscriptionStore{
				lookupErr:     testCase.lookupErr,
				messageErr:    testCase.messageErr,
				preference:    testCase.preference,
				preferenceErr: testCase.preferenceErr,
				mentionErr:    testCase.mentionErr,
			}
			notifier := newWebPushNotifier(sender, subscriptions, "")
			notification := pushNotificationFor("usr_1", "https://push.example.com/send/device")
			notification.MessageID = testCase.messageID
			if err := notifier.Notify(context.Background(), notification); err != nil {
				t.Fatal(err)
			}
			notifier.Close()
			if sender.count() != 0 {
				t.Fatalf("a refused delivery reached the push service %d times", sender.count())
			}
			if len(subscriptions.recorded()) != 0 {
				t.Fatalf("a refused delivery records nothing: %#v", subscriptions.recorded())
			}
			want := "web push delivery skipped for user usr_1: " + testCase.reason
			if !strings.Contains(captured.String(), want) {
				t.Fatalf("expected %q in the log, got %q", want, captured.String())
			}
			if strings.Contains(captured.String(), "push.example.com/send") {
				t.Fatalf("the log leaked the endpoint: %s", captured.String())
			}
		})
	}
}

// TestWebPushNotifierIgnoresChannelPreferenceForADirectMessage matches the
// recipient selection: a direct message reaches its members whatever they set
// for channels, so a muted preference must not stop it at send time either.
func TestWebPushNotifierIgnoresChannelPreferenceForADirectMessage(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	subscriptions := &fakeSubscriptionStore{
		message:    store.Message{DirectConversationID: "dm_1"},
		preference: store.ChannelNotifyMuted,
	}
	notifier := newWebPushNotifier(sender, subscriptions, "")
	if err := notifier.Notify(context.Background(), pushNotificationFor("usr_1", "https://push.example.com/send/device")); err != nil {
		t.Fatal(err)
	}
	notifier.Close()
	if sender.count() != 1 {
		t.Fatalf("a direct message was sent %d times, want 1", sender.count())
	}
}

// TestWebPushNotifierSendsTheKeysOnFileNow proves the queue does not carry
// device keys: a device re-registered while a push waits is sent under the
// keys the store holds at send time.
func TestWebPushNotifierSendsTheKeysOnFileNow(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	notifier := newWebPushNotifier(sender, &fakeSubscriptionStore{}, "")
	notification := pushNotificationFor("usr_1", "https://push.example.com/send/device")
	notification.Subscriptions[0].Auth = "queued-auth-is-not-used"
	if err := notifier.Notify(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	notifier.Close()
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.subscriptions) != 1 || sender.subscriptions[0].Auth != exampleClientAuth {
		t.Fatalf("expected the stored keys, sent %#v", sender.subscriptions)
	}
}

// gatedSender holds every push to a /hold/ endpoint until the gate opens and
// records the endpoints it actually sent to.
type gatedSender struct {
	mu       sync.Mutex
	gate     chan struct{}
	inFlight chan struct{}
	sent     []string
}

func (g *gatedSender) Send(_ context.Context, subscription webpush.Subscription, _ webpush.Message) error {
	if strings.Contains(subscription.Endpoint, "/hold/") {
		g.inFlight <- struct{}{}
		<-g.gate
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sent = append(g.sent, subscription.Endpoint)
	return nil
}

func (g *gatedSender) sentTo(endpoint string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	count := 0
	for _, sent := range g.sent {
		if sent == endpoint {
			count++
		}
	}
	return count
}

// TestQueuedWebPushIsRevalidatedBeforeSending holds every worker on a slow
// relay, queues a real message's push behind them, withdraws the recipient's
// authority, and then lets the relay go. The push that was already queued
// must not be sent; with nothing withdrawn, it is sent once.
func TestQueuedWebPushIsRevalidatedBeforeSending(t *testing.T) {
	t.Parallel()
	for name, withdraw := range map[string]func(t *testing.T, fixture queuedPushFixture){
		"control": func(*testing.T, queuedPushFixture) {},
		"session revoked": func(t *testing.T, fixture queuedPushFixture) {
			if err := fixture.store.RevokeSession(context.Background(), fixture.recipientSession); err != nil {
				t.Fatal(err)
			}
		},
		"subscription deleted": func(t *testing.T, fixture queuedPushFixture) {
			if err := fixture.store.DeletePushSubscription(context.Background(), fixture.recipient, fixture.recipientEndpoint); err != nil {
				t.Fatal(err)
			}
		},
		"message deleted": func(t *testing.T, fixture queuedPushFixture) {
			if _, _, err := fixture.store.DeleteMessage(context.Background(), store.DeleteMessageInput{
				MessageID: fixture.messageID, UserID: fixture.owner,
			}); err != nil {
				t.Fatal(err)
			}
		},
		"membership removed": func(t *testing.T, fixture queuedPushFixture) {
			// No endpoint removes a human member, so this is the row change an
			// operator would make; message access reads the same row.
			db, err := sql.Open("sqlite", "file:"+fixture.databasePath+"?_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`DELETE FROM workspace_members WHERE user_id = ?`, fixture.recipient); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newQueuedPushFixture(t)
			withdraw(t, fixture)
			close(fixture.sender.gate)
			fixture.notifier.Close()
			want := 0
			if name == "control" {
				want = 1
			}
			if got := fixture.sender.sentTo(fixture.recipientEndpoint); got != want {
				t.Fatalf("the recipient's device was sent %d pushes, want %d", got, want)
			}
		})
	}
}

type queuedPushFixture struct {
	store             *sqlitestore.Store
	databasePath      string
	server            *httptest.Server
	sender            *gatedSender
	notifier          *WebPushNotifier
	owner             string
	channelID         string
	messageID         string
	recipient         string
	recipientHandle   string
	recipientSession  string
	recipientEndpoint string
}

// newQueuedPushFixture returns with every worker held on the relay and the
// recipient's push for a freshly posted message waiting in the queue.
func newQueuedPushFixture(t *testing.T) queuedPushFixture {
	t.Helper()
	sender := &gatedSender{gate: make(chan struct{}), inFlight: make(chan struct{}, 4*webPushWorkers)}
	fixture := queuePushBehindHeldWorkers(t, "https://push.example.com", sender, sender.inFlight, sender.sentTo, queuedPost{})
	fixture.sender = sender
	return fixture
}

// queuedPost is what the owner posts and how the recipient is set to hear
// about it. The zero value posts queuedPushText to a recipient who hears
// about everything.
type queuedPost struct {
	// body builds the posted text from the recipient's handle.
	body       func(recipientHandle string) string
	preference string
}

const queuedPushText = "queued behind a slow relay"

// queuePushBehindHeldWorkers posts a message whose push for the recipient
// waits in the queue while every worker is held on a /hold/ endpoint under
// base. The sender decides what holding means; sent counts what reached an
// endpoint.
func queuePushBehindHeldWorkers(t *testing.T, base string, sender webPushSender, inFlight <-chan struct{}, sent func(endpoint string) int, post queuedPost) queuedPushFixture {
	t.Helper()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open("sqlite://" + databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "queued-owner@example.com")
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
	member := func(name, email string) string {
		user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: name, Email: email})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, user.ID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
		return user.ID
	}
	blocker := member("Blocker", "queued-blocker@example.com")
	recipient := member("Recipient", "queued-recipient@example.com")
	// Posted before any device exists, so it queues nothing on its own.
	warmUp, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "warm up"})
	if err != nil {
		t.Fatal(err)
	}
	register := func(userID, endpoint string) string {
		session, err := st.CreateSession(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertPushSubscription(ctx, store.PushSubscriptionInput{
			UserID: userID, Endpoint: endpoint, P256dh: exampleClientKey, Auth: exampleClientAuth, SessionToken: session.Token,
		}); err != nil {
			t.Fatal(err)
		}
		return session.Token
	}
	var blockers []store.PushSubscriptionTarget
	for index := range webPushWorkers {
		endpoint := base + "/hold/" + strconv.Itoa(index)
		register(blocker, endpoint)
		blockers = append(blockers, store.PushSubscriptionTarget{Endpoint: endpoint})
	}
	recipientEndpoint := base + "/send/recipient"
	recipientSession := register(recipient, recipientEndpoint)

	notifier := newWebPushNotifier(sender, st, "")
	t.Cleanup(notifier.Close)
	publicKey, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		WebPushNotifier:  notifier,
		WebPushPublicKey: publicKey,
	}).Handler())
	t.Cleanup(server.Close)

	if err := notifier.Notify(ctx, PushNotification{UserID: blocker, MessageID: warmUp.ID, Subscriptions: blockers}); err != nil {
		t.Fatal(err)
	}
	for range webPushWorkers {
		select {
		case <-inFlight:
		case <-time.After(5 * time.Second):
			t.Fatal("the workers never reached the held relay")
		}
	}
	// A created user has no handle, and a mention resolves only to a handle.
	handle := "recipient"
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{UserID: recipient, Handle: &handle}); err != nil {
		t.Fatal(err)
	}
	recipientUser, err := st.GetUser(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if post.preference != "" {
		if err := st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
			ChannelID: channels[0].ID, UserID: recipient, Preference: post.preference,
		}); err != nil {
			t.Fatal(err)
		}
	}
	text := queuedPushText
	if post.body != nil {
		text = post.body(recipientUser.Handle)
	}
	posted := postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channels[0].ID+"/messages", map[string]any{"body": text})
	if got := sent(recipientEndpoint); got != 0 {
		t.Fatalf("the recipient's push left before the workers were free: %d", got)
	}
	// A case that expects no push means something only if one was queued. The
	// blocker is a member too, so the post also queues one per held device.
	if waiting := len(notifier.queue); waiting != 1+webPushWorkers {
		t.Fatalf("%d pushes wait in the queue, want the recipient's and the blocker's %d", waiting, webPushWorkers)
	}
	return queuedPushFixture{
		store:             st,
		databasePath:      databasePath,
		server:            server,
		notifier:          notifier,
		owner:             owner.ID,
		channelID:         channels[0].ID,
		messageID:         posted.Message.ID,
		recipient:         recipient,
		recipientHandle:   recipientUser.Handle,
		recipientSession:  recipientSession,
		recipientEndpoint: recipientEndpoint,
	}
}

// TestQueuedWebPushCarriesTheTextAtSendTime queues a real message's push
// behind workers held on a push service, lets its author edit it through the
// API, and then lets the push service go. What the phone decrypts must be the
// message as it reads when the push is sent, not as it read when it queued.
func TestQueuedWebPushCarriesTheTextAtSendTime(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		edit string
		want string
	}{
		"edited while queued": {edit: "edited while it waited", want: "edited while it waited"},
		"unedited control":    {want: queuedPushText},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			relay := newHeldRelay(t)
			fixture := queuePushBehindHeldWorkers(t, relay.server.URL, relay.sender(t), relay.inFlight, relay.received, queuedPost{})
			if testCase.edit != "" {
				patchJSON[struct {
					Message store.Message `json:"message"`
				}](t, fixture.server.URL+"/api/messages/"+fixture.messageID, map[string]any{"body": testCase.edit})
			}
			relay.releaseHeld()
			fixture.notifier.Close()

			bodies := relay.bodies(fixture.recipientEndpoint)
			if len(bodies) != 1 {
				t.Fatalf("the recipient's device received %d pushes, want 1", len(bodies))
			}
			shown := openExamplePushPayload(t, bodies[0])
			if shown.UserID != fixture.recipient {
				t.Fatal("encrypted push did not identify its recipient")
			}
			if shown.Body != testCase.want {
				t.Fatalf("the phone shows %q, want %q", shown.Body, testCase.want)
			}
			if shown.Title != "Owner in #general" || shown.Tag != "clickclack:"+fixture.messageID {
				t.Fatalf("unexpected title or tag: %#v", shown)
			}
		})
	}
}

// TestQueuedWebPushRechecksTheRecipient queues a push behind held workers and
// then changes what chose its recipient: the author edits out the mention a
// mentions-only recipient was chosen for, or the recipient mutes the channel.
// The recipient must be chosen again, by the same rules, when the push is
// sent; one who would not be chosen now gets nothing, and the skip names why.
// Not parallel: it reads the process-wide log.
func TestQueuedWebPushRechecksTheRecipient(t *testing.T) {
	mention := func(handle string) string { return "@" + handle + " can you look at this" }
	for name, testCase := range map[string]struct {
		post   queuedPost
		change func(t *testing.T, fixture queuedPushFixture)
		want   string
		reason string
	}{
		"mention edited out": {
			post: queuedPost{body: mention, preference: store.ChannelNotifyMentions},
			change: func(t *testing.T, fixture queuedPushFixture) {
				patchJSON[struct {
					Message store.Message `json:"message"`
				}](t, fixture.server.URL+"/api/messages/"+fixture.messageID, map[string]any{"body": "never mind, sorted"})
			},
			reason: "the message does not mention the recipient",
		},
		"mention kept": {
			post: queuedPost{body: mention, preference: store.ChannelNotifyMentions},
			change: func(t *testing.T, fixture queuedPushFixture) {
				patchJSON[struct {
					Message store.Message `json:"message"`
				}](t, fixture.server.URL+"/api/messages/"+fixture.messageID, map[string]any{"body": "@" + fixture.recipientHandle + " still you"})
			},
			want: "@RECIPIENT still you",
		},
		"muted while queued": {
			change: func(t *testing.T, fixture queuedPushFixture) {
				request, err := http.NewRequest(http.MethodPatch,
					fixture.server.URL+"/api/channels/"+fixture.channelID+"/notification-settings",
					strings.NewReader(`{"preference":"muted"}`))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-ClickClack-User", fixture.recipient)
				doJSON[map[string]any](t, request)
			},
			reason: "the recipient muted the channel",
		},
	} {
		t.Run(name, func(t *testing.T) {
			captured := captureLog(t)
			relay := newHeldRelay(t)
			fixture := queuePushBehindHeldWorkers(t, relay.server.URL, relay.sender(t), relay.inFlight, relay.received, testCase.post)
			testCase.change(t, fixture)
			relay.releaseHeld()
			fixture.notifier.Close()

			bodies := relay.bodies(fixture.recipientEndpoint)
			if testCase.reason != "" {
				if len(bodies) != 0 {
					t.Fatalf("a recipient the rules no longer choose received %d pushes: %q", len(bodies), openExamplePushPayload(t, bodies[0]).Body)
				}
				want := "web push delivery skipped for user " + fixture.recipient + ": " + testCase.reason
				if !strings.Contains(captured.String(), want) {
					t.Fatalf("expected %q in the log, got %q", want, captured.String())
				}
				return
			}
			if len(bodies) != 1 {
				t.Fatalf("the recipient's device received %d pushes, want 1", len(bodies))
			}
			want := strings.ReplaceAll(testCase.want, "RECIPIENT", fixture.recipientHandle)
			if shown := openExamplePushPayload(t, bodies[0]); shown.Body != want {
				t.Fatalf("the phone shows %q, want %q", shown.Body, want)
			}
		})
	}
}

// heldRelay is a push service that holds every request to a /hold/ path until
// released and keeps the body of every other request, so a test can queue a
// push behind busy workers and read what the phone would be sent.
type heldRelay struct {
	server   *httptest.Server
	inFlight chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	kept     map[string][][]byte
}

func newHeldRelay(t *testing.T) *heldRelay {
	t.Helper()
	relay := &heldRelay{
		inFlight: make(chan struct{}, 4*webPushWorkers),
		release:  make(chan struct{}),
		kept:     map[string][][]byte{},
	}
	relay.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(r.URL.Path, "/hold/") {
			relay.inFlight <- struct{}{}
			<-relay.release
		} else {
			relay.mu.Lock()
			relay.kept[r.URL.Path] = append(relay.kept[r.URL.Path], body)
			relay.mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(func() {
		relay.releaseHeld()
		relay.server.Close()
	})
	return relay
}

// sender is the real push sender, pointed at this relay.
func (h *heldRelay) sender(t *testing.T) *webpush.Sender {
	t.Helper()
	publicKey, privateKey, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	return &webpush.Sender{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    "https://chat.example.com",
		Client:     h.server.Client(),
	}
}

func (h *heldRelay) releaseHeld() {
	h.once.Do(func() { close(h.release) })
}

func (h *heldRelay) bodies(endpoint string) [][]byte {
	path := strings.TrimPrefix(endpoint, h.server.URL)
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]byte(nil), h.kept[path]...)
}

func (h *heldRelay) received(endpoint string) int {
	return len(h.bodies(endpoint))
}

// exampleClientPrivateKey is the RFC 8291 Appendix A subscription private key,
// the other half of exampleClientKey.
const exampleClientPrivateKey = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"

// openExamplePushPayload does what the phone does with one push sent to the
// example subscription: RFC 8291 key agreement, then the RFC 8188 record.
func openExamplePushPayload(t *testing.T, body []byte) webpush.Message {
	t.Helper()
	decode := func(value string) []byte {
		raw, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	clientKey, err := ecdh.P256().NewPrivateKey(decode(exampleClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	const saltLength = 16
	if len(body) < saltLength+5 {
		t.Fatalf("the push body is %d bytes, too short for a record header", len(body))
	}
	keyLength := int(body[saltLength+4])
	salt := body[:saltLength]
	senderPublicKey := body[saltLength+5 : saltLength+5+keyLength]
	senderPublic, err := ecdh.P256().NewPublicKey(senderPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sharedSecret, err := clientKey.ECDH(senderPublic)
	if err != nil {
		t.Fatal(err)
	}
	keyInfo := append([]byte("WebPush: info\x00"), clientKey.PublicKey().Bytes()...)
	keyInfo = append(keyInfo, senderPublicKey...)
	combiningKey, err := hkdf.Extract(sha256.New, sharedSecret, decode(exampleClientAuth))
	if err != nil {
		t.Fatal(err)
	}
	inputKeyingMaterial, err := hkdf.Expand(sha256.New, combiningKey, string(keyInfo), sha256.Size)
	if err != nil {
		t.Fatal(err)
	}
	contentPRK, err := hkdf.Extract(sha256.New, inputKeyingMaterial, salt)
	if err != nil {
		t.Fatal(err)
	}
	contentKey, err := hkdf.Expand(sha256.New, contentPRK, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := hkdf.Expand(sha256.New, contentPRK, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	record, err := aead.Open(nil, nonce, body[saltLength+5+keyLength:], nil)
	if err != nil {
		t.Fatalf("the push does not open under the subscription's keys: %v", err)
	}
	if len(record) == 0 || record[len(record)-1] != 0x02 {
		t.Fatalf("the record is not the last one: %x", record)
	}
	var message webpush.Message
	if err := json.Unmarshal(record[:len(record)-1], &message); err != nil {
		t.Fatal(err)
	}
	return message
}

// newWebPushTestStore opens a migrated SQLite store with a bootstrapped owner
// and the default workspace's first channel, and returns the database file.
func newWebPushTestStore(t *testing.T) (*sqlitestore.Store, store.User, store.Channel, string) {
	t.Helper()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open("sqlite://" + databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "keyed-owner@example.com")
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
	return st, owner, channels[0], databasePath
}

func registerKeyedDevice(t *testing.T, st *sqlitestore.Store, userID, endpoint, keyID string) {
	t.Helper()
	session, err := st.CreateSession(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(context.Background(), store.PushSubscriptionInput{
		UserID: userID, Endpoint: endpoint, P256dh: exampleClientKey, Auth: exampleClientAuth,
		SessionToken: session.Token, KeyID: keyID,
	}); err != nil {
		t.Fatal(err)
	}
}

// capturedLog is the standard logger's output, safe to read while a
// background goroutine is still writing to it.
type capturedLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (c *capturedLog) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text.Write(p)
}

func (c *capturedLog) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text.String()
}

// captureLog sends the standard logger to a buffer for the rest of the test.
// Tests that use it are not parallel: the logger is process wide.
func captureLog(t *testing.T) *capturedLog {
	t.Helper()
	captured := &capturedLog{}
	previousOutput := log.Writer()
	log.SetOutput(captured)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	return captured
}

// TestWebPushSkipsADeviceUnderARetiredKey is a push already queued for a
// device when the server's key changes: the worker's re-read refuses it, the
// push service is never called, and the row is left for the sweep.
func TestWebPushSkipsADeviceUnderARetiredKey(t *testing.T) {
	st, owner, channel, _ := newWebPushTestStore(t)
	ctx := context.Background()
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "for the old key"})
	if err != nil {
		t.Fatal(err)
	}
	const retired, current = "https://push.example.com/send/retired", "https://push.example.com/send/current"
	registerKeyedDevice(t, st, owner.ID, retired, "key-before")
	registerKeyedDevice(t, st, owner.ID, current, "key-now")
	captured := captureLog(t)

	sender := &fakeSender{}
	notifier := newWebPushNotifier(sender, st, "key-now")
	for _, endpoint := range []string{retired, current} {
		if err := notifier.Notify(ctx, PushNotification{
			UserID: owner.ID, MessageID: message.ID, Subscriptions: []store.PushSubscriptionTarget{{Endpoint: endpoint}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	notifier.Close()
	sender.mu.Lock()
	sent := append([]webpush.Subscription(nil), sender.subscriptions...)
	sender.mu.Unlock()
	if len(sent) != 1 || sent[0].Endpoint != current {
		t.Fatalf("only the device under the current key may be sent to, sent %d", len(sent))
	}
	want := "web push delivery skipped for user " + owner.ID + ": the device was registered under a retired key"
	if !strings.Contains(captured.String(), want) {
		t.Fatalf("expected %q in the log, got %q", want, captured.String())
	}
	if strings.Contains(captured.String(), "push.example.com/send") {
		t.Fatalf("the log leaked an endpoint: %s", captured.String())
	}
	subscriptions, err := st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil || len(subscriptions) != 2 {
		t.Fatalf("a skip removes nothing: %#v %v", subscriptions, err)
	}
}

// TestWebPushRecipientSelectionSkipsRetiredKeys posts a real message to a
// member with three devices: one under the server's key, one under a key it
// retired, and one registered before keys were recorded. Only the retired one
// is left out of the notification.
func TestWebPushRecipientSelectionSkipsRetiredKeys(t *testing.T) {
	t.Parallel()
	st, _, channel, _ := newWebPushTestStore(t)
	ctx := context.Background()
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "keyed-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, channel.WorkspaceID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	const current, retired, unrecorded = "https://push.example.com/send/current", "https://push.example.com/send/retired", "https://push.example.com/send/unrecorded"
	registerKeyedDevice(t, st, member.ID, current, webPushKeyID(publicKey))
	registerKeyedDevice(t, st, member.ID, retired, "0123456789ab")
	registerKeyedDevice(t, st, member.ID, unrecorded, "")
	web := &recordingNotifier{}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{WebPushNotifier: web, WebPushPublicKey: publicKey}).Handler())
	t.Cleanup(server.Close)

	postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]any{"body": "which devices ring"})
	if len(web.notifications) != 1 {
		t.Fatalf("expected one web push call, got %d", len(web.notifications))
	}
	var endpoints []string
	for _, target := range web.notifications[0].Subscriptions {
		endpoints = append(endpoints, target.Endpoint)
	}
	sort.Strings(endpoints)
	if strings.Join(endpoints, " ") != current+" "+unrecorded {
		t.Fatalf("selected %v, want the current and the unrecorded device", endpoints)
	}

	for _, endpoint := range []string{current, unrecorded} {
		if err := st.DeletePushSubscription(ctx, member.ID, endpoint); err != nil {
			t.Fatal(err)
		}
	}
	postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]any{"body": "only a retired device left"})
	if len(web.notifications) != 1 {
		t.Fatalf("a member with only a retired device gets no web push call, got %d", len(web.notifications))
	}
}

// TestWebPushNotifierSweepsDeadDevicesAtStart seeds a device whose session was
// revoked and one under a key retired a month ago beside a live device. The
// sweep runs as the notifier starts, long before its first tick.
func TestWebPushNotifierSweepsDeadDevicesAtStart(t *testing.T) {
	st, owner, _, databasePath := newWebPushTestStore(t)
	ctx := context.Background()
	const live, revoked, retired = "https://push.example.com/send/live", "https://push.example.com/send/revoked", "https://push.example.com/send/retired"
	registerKeyedDevice(t, st, owner.ID, live, "key-now")
	session, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, store.PushSubscriptionInput{
		UserID: owner.ID, Endpoint: revoked, P256dh: exampleClientKey, Auth: exampleClientAuth, SessionToken: session.Token, KeyID: "key-now",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	registerKeyedDevice(t, st, owner.ID, retired, "key-before")
	db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	monthAgo := time.Now().UTC().Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE user_push_subscriptions SET updated_at = ? WHERE endpoint = ?`, monthAgo, retired); err != nil {
		t.Fatal(err)
	}
	captured := captureLog(t)

	notifier := newWebPushNotifier(&fakeSender{}, st, "key-now")
	defer notifier.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		subscriptions, err := st.ListPushSubscriptions(ctx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(subscriptions) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sweep did not run at start: %d devices left", len(subscriptions))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, owner.ID, live, "key-now"); err != nil {
		t.Fatalf("the live device must survive the sweep: %v", err)
	}
	want := "web push pruned 2 devices: 0 refused by their push service for a week, 1 under a retired key, 1 whose session ended"
	for !strings.Contains(captured.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("expected %q in the log, got %q", want, captured.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWebPushNotifierSweepStopsOnClose watches the sweep repeat on a short
// tick, closes the notifier, and checks no pass runs after Close returns: the
// sweep's goroutine has exited. Not parallel: it shortens the package tick.
func TestWebPushNotifierSweepStopsOnClose(t *testing.T) {
	previousInterval := webPushPruneInterval
	webPushPruneInterval = 5 * time.Millisecond
	defer func() { webPushPruneInterval = previousInterval }()

	subscriptions := &fakeSubscriptionStore{}
	notifier := newWebPushNotifier(&fakeSender{}, subscriptions, "key-now")
	deadline := time.Now().Add(5 * time.Second)
	for subscriptions.pruneCount() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the sweep did not repeat: %d passes", subscriptions.pruneCount())
		}
		time.Sleep(time.Millisecond)
	}
	notifier.Close()
	passes := subscriptions.pruneCount()
	time.Sleep(50 * time.Millisecond)
	if got := subscriptions.pruneCount(); got != passes {
		t.Fatalf("the sweep ran %d more times after Close", got-passes)
	}
}

// TestWebPushNotifierSurvivesAFailingSweep guards the detached sweep the way
// the delivery pool is guarded: a store error is logged, and a panic is
// recovered with one line instead of taking the server down.
func TestWebPushNotifierSurvivesAFailingSweep(t *testing.T) {
	for name, testCase := range map[string]struct {
		subscriptions *fakeSubscriptionStore
		want          string
	}{
		"error": {&fakeSubscriptionStore{pruneErr: errors.New("database is away")}, "web push prune failed: database is away"},
		"panic": {&fakeSubscriptionStore{prunePanic: true}, "web push prune panicked: prune exploded"},
	} {
		t.Run(name, func(t *testing.T) {
			captured := captureLog(t)
			notifier := newWebPushNotifier(&fakeSender{}, testCase.subscriptions, "key-now")
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(captured.String(), testCase.want) {
				if time.Now().After(deadline) {
					t.Fatalf("expected %q in the log, got %q", testCase.want, captured.String())
				}
				time.Sleep(time.Millisecond)
			}
			notifier.Close()
		})
	}
}
