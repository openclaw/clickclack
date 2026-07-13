package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

type staticCallbackResolver map[string][]netip.Addr

func (r staticCallbackResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	ips, ok := r[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return ips, nil
}

func configureLocalCallbackPolicy(server *Server) {
	dialer := &net.Dialer{}
	policy := &callbackNetworkPolicy{
		resolver: staticCallbackResolver{
			"callback.test": {netip.MustParseAddr("93.184.216.34")},
		},
		dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		},
	}
	server.callbackPolicy = policy
	server.callbackClient = policy.client()
}

func localCallbackURL(rawURL string) string {
	parsed, _ := url.Parse(rawURL)
	return "http://callback.test:" + parsed.Port()
}

func TestCallbackNetworkPolicyBlocksNonPublicDestinationsAndPinsDial(t *testing.T) {
	t.Parallel()
	resolver := staticCallbackResolver{
		"public.example":  {netip.MustParseAddr("93.184.216.34")},
		"mixed.example":   {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")},
		"private.example": {netip.MustParseAddr("192.168.1.10")},
	}
	var dialed string
	policy := &callbackNetworkPolicy{
		resolver: resolver,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = address
			return nil, errors.New("stop after address capture")
		},
	}
	for _, rawURL := range []string{
		"http://127.0.0.1/hook",
		"http://[::1]/hook",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/hook",
		"http://192.168.1.10/hook",
		"http://metadata.google.internal/computeMetadata/v1",
		"http://private.example/hook",
		"http://mixed.example/hook",
	} {
		if err := policy.validateURL(context.Background(), rawURL); err == nil {
			t.Errorf("expected %s to be rejected", rawURL)
		}
	}
	if err := policy.validateURL(context.Background(), "https://public.example/hook"); err != nil {
		t.Fatalf("public callback rejected: %v", err)
	}
	if _, err := policy.dial(context.Background(), "tcp", "public.example:443"); err == nil {
		t.Fatal("expected capture dial to return its sentinel error")
	}
	if dialed != "93.184.216.34:443" {
		t.Fatalf("callback dial was not pinned to resolved IP: %q", dialed)
	}
}

func TestCallbackClientDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Location", "http://169.254.169.254/latest/meta-data")
		w.WriteHeader(http.StatusFound)
	}))
	defer target.Close()

	server := &Server{}
	configureLocalCallbackPolicy(server)
	req, err := http.NewRequest(http.MethodPost, localCallbackURL(target.URL), strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.callbackClient.Do(req); err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("redirect target was requested: count=%d", requests.Load())
	}
}

func TestTopicBotWorkspaceIsolationAndPartialProfileUpdates(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-security@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstWorkspace := workspaces[0]
	secondWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Second Workspace"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: firstWorkspace.ID,
		DisplayName: "Scoped Bot",
		TokenName:   "security",
		Scopes:      []string{"channels:read", "channels:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bot.ID == "" {
		t.Fatal("expected bot")
	}
	serverState := New(st, realtime.NewHub(), Options{})
	server := httptest.NewServer(serverState.Handler())
	t.Cleanup(serverState.Close)
	t.Cleanup(server.Close)

	securityExpectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/workspaces/"+secondWorkspace.ID+"/topics", nil, http.StatusForbidden)
	securityExpectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/workspaces/"+secondWorkspace.ID+"/topics", strings.NewReader(`{"name":"escape"}`), http.StatusForbidden)
	securityExpectStatusAsUser(t, owner.ID, http.MethodPost, server.URL+"/api/workspaces/"+firstWorkspace.ID+"/event-subscriptions", strings.NewReader(`{"event_types":["message.created"],"callback_url":"http://169.254.169.254/latest/meta-data"}`), http.StatusBadRequest)
	securityExpectStatusAsUser(t, owner.ID, http.MethodPost, server.URL+"/api/workspaces/"+firstWorkspace.ID+"/slash-commands", strings.NewReader(`{"command":"/unsafe","callback_url":"http://127.0.0.1/callback","bot_user_id":"`+bot.ID+`"}`), http.StatusBadRequest)

	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      owner.ID,
		DisplayName: "Concurrent Profile",
		Handle:      "concurrent",
		AvatarURL:   "https://example.com/concurrent.png",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          owner.ID,
		PushoverEnabled: true,
		PushoverUserKey: "u12345678901234567890123456789",
	}); err != nil {
		t.Fatal(err)
	}
	profileOnly := securityPatchJSON[struct {
		User store.User `json:"user"`
	}](t, server.URL+"/api/me", map[string]any{"display_name": "Profile Only"})
	if profileOnly.User.NotificationSettings == nil || !profileOnly.User.NotificationSettings.PushoverEnabled || profileOnly.User.NotificationSettings.PushoverUserKey == "" {
		t.Fatalf("profile-only update overwrote notifications: %#v", profileOnly.User)
	}
	if profileOnly.User.Handle != "concurrent" || profileOnly.User.AvatarURL != "https://example.com/concurrent.png" {
		t.Fatalf("profile-only update overwrote omitted profile fields: %#v", profileOnly.User)
	}

	notificationOnly := securityPatchJSON[struct {
		User store.User `json:"user"`
	}](t, server.URL+"/api/me", map[string]any{
		"notification_settings": map[string]any{"pushover_enabled": false},
	})
	if notificationOnly.User.DisplayName != "Profile Only" || notificationOnly.User.Handle != "concurrent" || notificationOnly.User.AvatarURL != "https://example.com/concurrent.png" {
		t.Fatalf("notification-only update overwrote profile: %#v", notificationOnly.User)
	}
	if notificationOnly.User.NotificationSettings == nil || notificationOnly.User.NotificationSettings.PushoverEnabled || notificationOnly.User.NotificationSettings.PushoverUserKey == "" {
		t.Fatalf("notification-only partial update did not preserve omitted key: %#v", notificationOnly.User.NotificationSettings)
	}
}

func TestEventCallbacksDoNotBlockMutationResponses(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-async-callbacks@example.com")
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

	release := make(chan struct{})
	callbackStarted := make(chan struct{}, eventDeliveryWorkerCount)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callbackStarted <- struct{}{}
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()

	serverState := New(st, realtime.NewHub(), Options{})
	configureLocalCallbackPolicy(serverState)
	server := httptest.NewServer(serverState.Handler())
	t.Cleanup(serverState.Close)
	t.Cleanup(server.Close)
	for range 8 {
		if _, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
			WorkspaceID: workspaces[0].ID,
			EventTypes:  []string{"message.created"},
			CallbackURL: localCallbackURL(callback.URL),
			CreatedBy:   owner.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	startedAt := time.Now()
	securityPostJSONAsUser[struct {
		Message store.Message `json:"message"`
	}](t, owner.ID, server.URL+"/api/channels/"+channels[0].ID+"/messages", map[string]string{"body": "fast mutation"})
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("mutation waited for callbacks: %s", elapsed)
	}
	for range eventDeliveryWorkerCount {
		select {
		case <-callbackStarted:
		case <-time.After(time.Second):
			t.Fatal("event callback workers did not start")
		}
	}
	close(release)
}

func TestEventDeliveryQueueAppliesBackpressure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := &eventDeliveryDispatcher{
		ctx:    ctx,
		cancel: cancel,
		queue:  make(chan eventDeliveryJob, eventDeliveryQueueSize),
	}
	dispatcher.startOnce.Do(func() {})
	for i := 0; i < eventDeliveryQueueSize; i++ {
		if !dispatcher.enqueue(eventDeliveryJob{}) {
			t.Fatalf("queue rejected job %d before capacity", i)
		}
	}
	if dispatcher.enqueue(eventDeliveryJob{}) {
		t.Fatal("queue accepted work beyond its configured capacity")
	}
	dispatcher.close()
}

func securityExpectStatusWithBearer(t *testing.T, token, method, endpoint string, body io.Reader, status int) {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: expected %d, got %s %s", method, endpoint, status, resp.Status, string(payload))
	}
}

func securityExpectStatusAsUser(t *testing.T, userID, method, endpoint string, body io.Reader, status int) {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: expected %d, got %s %s", method, endpoint, status, resp.Status, string(payload))
	}
}

func securityPatchJSON[T any](t *testing.T, endpoint string, body any) T {
	t.Helper()
	return securityJSONRequest[T](t, "", http.MethodPatch, endpoint, body)
}

func securityPostJSONAsUser[T any](t *testing.T, userID, endpoint string, body any) T {
	t.Helper()
	return securityJSONRequest[T](t, userID, http.MethodPost, endpoint, body)
}

func securityJSONRequest[T any](t *testing.T, userID, method, endpoint string, body any) T {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-ClickClack-User", userID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: %s %s", method, endpoint, resp.Status, string(responseBody))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
