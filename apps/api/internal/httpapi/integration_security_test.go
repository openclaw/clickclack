package httpapi

import (
	"context"
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

func TestRegisteredSlashCommandHonorsCallerModeration(t *testing.T) {
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

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "slash-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "slash-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "slash-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Command Bot", CreatedBy: moderator.ID})
	if err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var guestChannelID, generalChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "guest":
			guestChannelID = channel.ID
		case "general":
			generalChannelID = channel.ID
		}
	}
	if guestChannelID == "" || generalChannelID == "" {
		t.Fatalf("expected guest and general channels, got %#v", channels)
	}

	var callbackCount atomic.Int32
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callbackCount.Add(1)
		writeJSON(w, http.StatusOK, map[string]string{"response_type": "ephemeral", "text": "ok"})
	}))
	t.Cleanup(callback.Close)
	if _, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID: workspace.ID,
		Command:     "/deploy",
		CallbackURL: callback.URL,
		BotUserID:   bot.ID,
		CreatedBy:   moderator.ID,
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		UploadDir:      filepath.Join(dataDir, "uploads"),
		callbackClient: &http.Client{Timeout: callbackTimeout},
	}).Handler())
	t.Cleanup(server.Close)

	expectSlashStatusAsUser(t, guest.ID, server.URL+"/api/hooks/slash/"+generalChannelID, http.StatusForbidden)
	if got := callbackCount.Load(); got != 0 {
		t.Fatalf("hidden-channel guest invocation reached callback %d times", got)
	}
	expectSlashStatusAsUser(t, guest.ID, server.URL+"/api/hooks/slash/"+guestChannelID, http.StatusOK)
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("eligible guest invocation reached callback %d times, want 1", got)
	}

	timeoutUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, TimeoutUntil: &timeoutUntil}); err != nil {
		t.Fatal(err)
	}
	expectSlashStatusAsUser(t, guest.ID, server.URL+"/api/hooks/slash/"+guestChannelID, http.StatusForbidden)
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("timed-out guest invocation reached callback %d times", got)
	}
	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, ClearTimeout: true, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	expectSlashStatusAsUser(t, guest.ID, server.URL+"/api/hooks/slash/"+guestChannelID, http.StatusForbidden)
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("blocked guest invocation reached callback %d times", got)
	}
	blocked = false
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, ClearTimeout: true, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}

	blocked = true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: member.ID, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	expectSlashStatusAsUser(t, member.ID, server.URL+"/api/hooks/slash/"+generalChannelID, http.StatusForbidden)
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("blocked member invocation reached callback %d times", got)
	}

	for i := 0; i < store.GuestPostLimit; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: guest.ID, Body: "budget"}); err != nil {
			t.Fatal(err)
		}
	}
	expectSlashStatusAsUser(t, guest.ID, server.URL+"/api/hooks/slash/"+guestChannelID, http.StatusTooManyRequests)
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("rate-limited guest invocation reached callback %d times", got)
	}
}

func TestCallbackAddressPolicy(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"8.8.8.8":              true,
		"2606:4700:4700::1111": true,
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"169.254.169.254":      false,
		"100.64.0.1":           false,
		"198.18.0.1":           false,
		"192.0.2.1":            false,
		"::1":                  false,
		"fc00::1":              false,
		"fe80::1":              false,
		"fe80::1%eth0":         false,
		"fec0::1":              false,
		"fec0::1%eth0":         false,
		"::192.168.1.1":        false,
		"::ffff:127.0.0.1":     false,
		"64:ff9b::c0a8:101":    false,
		"64:ff9b:1::c0a8:101":  false,
		"2001::7f00:1":         false,
		"2001:db8::1":          false,
		"2002:7f00:1::":        false,
		"3ffe::1":              false,
		"3fff::1":              false,
	}
	for raw, want := range tests {
		address := netip.MustParseAddr(raw)
		if got := isPublicCallbackAddr(address); got != want {
			t.Errorf("isPublicCallbackAddr(%s) = %v, want %v", address, got, want)
		}
	}
}

func TestCallbackDialerRejectsMixedDNSAnswersBeforeDial(t *testing.T) {
	t.Parallel()
	var dialed atomic.Bool
	dialer := &callbackDialer{
		lookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed.Store(true)
			return nil, errors.New("unexpected dial")
		},
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "callback.example:443"); err == nil {
		t.Fatal("expected mixed public/private DNS answer to be rejected")
	}
	if dialed.Load() {
		t.Fatal("callback dialer connected before validating every DNS answer")
	}
}

func TestCallbackDialerPinsValidatedAddress(t *testing.T) {
	t.Parallel()
	const wantAddress = "8.8.8.8:443"
	var gotAddress string
	dialer := &callbackDialer{
		lookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		},
		dialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			gotAddress = address
			return nil, errors.New("sentinel")
		},
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "callback.example:443"); err == nil {
		t.Fatal("expected sentinel dial error")
	}
	if gotAddress != wantAddress {
		t.Fatalf("dialed %q, want pinned address %q", gotAddress, wantAddress)
	}
}

func TestCallbackClientDisablesRedirectsAndEnvironmentProxy(t *testing.T) {
	t.Parallel()
	client := newCallbackHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("callback client permits default redirects")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("unexpected redirect policy: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("callback client inherits environment proxy configuration")
	}
}

func TestCallbackDeliveryBlocksLoopbackForBothCallbackTypes(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(target.Close)

	server := &Server{callbackClient: newCallbackHTTPClient()}
	if _, _, err := server.postSlashCallback(context.Background(), store.SlashCommand{
		CallbackURL:   target.URL,
		SigningSecret: "slash-secret",
	}, []byte(`{"command":"/probe"}`)); err == nil {
		t.Fatal("slash callback reached a loopback target")
	}
	if _, _, err := server.postEventCallback(context.Background(), store.EventSubscription{
		CallbackURL:   target.URL,
		SigningSecret: "event-secret",
	}, store.Event{ID: "evt_probe"}, []byte(`{"event":"probe"}`)); err == nil {
		t.Fatal("event callback reached a loopback target")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("loopback callback server received %d requests", got)
	}
}

func expectSlashStatusAsUser(t *testing.T, userID, endpoint string, status int) {
	t.Helper()
	body := url.Values{"command": {"/deploy"}, "text": {"prod"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s as %s: expected %d, got %s %s", endpoint, userID, status, resp.Status, string(payload))
	}
}
