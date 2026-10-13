package httpapi

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
	"github.com/openclaw/clickclack/apps/api/internal/webpush"
)

// exampleClientKey is the RFC 8291 Appendix A subscription public key: a real
// point on P-256, so registration accepts it.
const (
	exampleClientKey  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	exampleClientAuth = "BTBZMqHH6r4Tts7J_aSIgg"
)

type pushTestServer struct {
	server *httptest.Server
	store  *sqlitestore.Store
	owner  store.User
	keyID  string
}

func newPushTestServer(t *testing.T, enabled bool) pushTestServer {
	t.Helper()
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-endpoints@example.com")
	if err != nil {
		t.Fatal(err)
	}
	options := Options{}
	if enabled {
		publicKey, _, err := webpush.GenerateKeys()
		if err != nil {
			t.Fatal(err)
		}
		options.WebPushPublicKey = publicKey
		options.WebPushNotifier = &recordingNotifier{}
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), options).Handler())
	t.Cleanup(server.Close)
	return pushTestServer{server: server, store: st, owner: owner, keyID: webPushKeyID(options.WebPushPublicKey)}
}

func TestPushEndpointsRegisterAndRemoveADevice(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, true)
	state := getJSON[struct {
		Enabled       bool                     `json:"enabled"`
		VAPIDKey      string                   `json:"vapid_public_key"`
		Subscriptions []store.PushSubscription `json:"subscriptions"`
	}](t, fixture.server.URL+"/api/me/push")
	if !state.Enabled || state.VAPIDKey == "" || len(state.Subscriptions) != 0 {
		t.Fatalf("unexpected initial state %#v", state)
	}

	endpoint := "https://push.example.com/send/device-one"
	stored := putPushSubscription(t, fixture.server.URL, fixture.owner.ID, endpoint, "iPhone")
	if stored.Subscription.UserAgent != "iPhone" || stored.Subscription.ID == "" {
		t.Fatalf("unexpected stored summary %#v", stored.Subscription)
	}
	body, err := json.Marshal(stored.Subscription)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"endpoint", "p256dh", "auth", endpoint, exampleClientKey, exampleClientAuth} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("the summary must not carry %q: %s", secret, body)
		}
	}

	state = getJSON[struct {
		Enabled       bool                     `json:"enabled"`
		VAPIDKey      string                   `json:"vapid_public_key"`
		Subscriptions []store.PushSubscription `json:"subscriptions"`
	}](t, fixture.server.URL+"/api/me/push")
	if len(state.Subscriptions) != 1 {
		t.Fatalf("expected the device to be listed, got %#v", state.Subscriptions)
	}

	removal := `{"user_id":"` + fixture.owner.ID + `","endpoint":"` + endpoint + `"}`
	expectStatus(t, http.MethodDelete, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(removal), http.StatusNoContent)
	expectStatus(t, http.MethodDelete, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(removal), http.StatusNoContent)
	state = getJSON[struct {
		Enabled       bool                     `json:"enabled"`
		VAPIDKey      string                   `json:"vapid_public_key"`
		Subscriptions []store.PushSubscription `json:"subscriptions"`
	}](t, fixture.server.URL+"/api/me/push")
	if len(state.Subscriptions) != 0 {
		t.Fatalf("expected the device to be gone, got %#v", state.Subscriptions)
	}
}

// A browser holds one subscription for every account on it, so whether the
// switch is on is a question about this device, not about the account's
// device count. The client asks with a digest of its endpoint; the endpoint
// itself never crosses the wire in either direction.
func TestPushStateAnswersForThisDevice(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, true)
	// The unpadded base64url SHA-256 of each endpoint, computed outside Go so
	// the server and the browser are held to one encoding.
	const (
		thisEndpoint  = "https://push.example.com/send/this-device"
		thisKey       = "roAu0n9u864S8b50dqaahZyGG6xEblpUS-e4wI47j74"
		otherEndpoint = "https://push.example.com/send/other-device"
		otherKey      = "QRwuTHGa0ab-lUQ-J0DAANtRKTDtZ80bqlhAbRDCtTI"
	)
	type pushState struct {
		Subscriptions []store.PushSubscription `json:"subscriptions"`
		ThisDevice    bool                     `json:"this_device"`
	}
	read := func(query string) (pushState, string) {
		t.Helper()
		response, err := http.Get(fixture.server.URL + "/api/me/push" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/me/push%s: %d %s", query, response.StatusCode, raw)
		}
		var state pushState
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		return state, string(raw)
	}

	if state, _ := read("?device=" + thisKey); state.ThisDevice {
		t.Fatal("no device is registered yet")
	}
	putPushSubscription(t, fixture.server.URL, fixture.owner.ID, otherEndpoint, "phone")
	if state, _ := read("?device=" + thisKey); state.ThisDevice || len(state.Subscriptions) != 1 {
		t.Fatalf("a device elsewhere is not this one: %#v", state)
	}
	putPushSubscription(t, fixture.server.URL, fixture.owner.ID, thisEndpoint, "laptop")
	state, raw := read("?device=" + thisKey)
	if !state.ThisDevice {
		t.Fatalf("the registered endpoint must read as this device: %s", raw)
	}
	for _, secret := range []string{thisEndpoint, otherEndpoint, thisKey, otherKey} {
		if strings.Contains(raw, secret) {
			t.Fatalf("the state must not carry %q: %s", secret, raw)
		}
	}
	if state, _ := read("?device=" + otherKey); !state.ThisDevice {
		t.Fatal("each registered endpoint answers for itself")
	}
	if state, _ := read(""); state.ThisDevice {
		t.Fatal("a caller that names no device has none")
	}
	if state, _ := read("?device=" + thisEndpoint); state.ThisDevice {
		t.Fatal("only the digest identifies a device, never the endpoint itself")
	}
}

// A device registered under a key the server no longer signs with cannot
// receive, and a browser that hides its subscription's key cannot tell. The
// state names such a device stale so the app replaces its subscription; it
// says so only for the device asking, and never names a key or an endpoint.
func TestPushStateNamesThisDeviceStaleUnderARetiredKey(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, true)
	const (
		currentEndpoint = "https://push.example.com/send/this-device"
		currentKey      = "roAu0n9u864S8b50dqaahZyGG6xEblpUS-e4wI47j74"
		retiredEndpoint = "https://push.example.com/send/other-device"
		retiredKey      = "QRwuTHGa0ab-lUQ-J0DAANtRKTDtZ80bqlhAbRDCtTI"
		unknownKey      = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		retiredKeyID    = "0123456789ab"
	)
	putPushSubscription(t, fixture.server.URL, fixture.owner.ID, currentEndpoint, "laptop")
	listed, err := fixture.store.ListPushSubscriptions(context.Background(), fixture.owner.ID)
	if err != nil || len(listed) != 1 || fixture.keyID == "" || listed[0].KeyID != fixture.keyID {
		t.Fatalf("a registration records the key the server signs with, %q: %#v %v", fixture.keyID, listed, err)
	}
	if _, err := fixture.store.UpsertPushSubscription(context.Background(), store.PushSubscriptionInput{
		UserID: fixture.owner.ID, Endpoint: retiredEndpoint, P256dh: exampleClientKey, Auth: exampleClientAuth,
		UserAgent: "phone", DevelopmentActor: true, KeyID: retiredKeyID,
	}); err != nil {
		t.Fatal(err)
	}

	type pushState struct {
		ThisDevice      bool `json:"this_device"`
		ThisDeviceStale bool `json:"this_device_stale"`
	}
	read := func(baseURL, query string) (pushState, string) {
		t.Helper()
		response, err := http.Get(baseURL + "/api/me/push" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"this_device_stale":`) {
			t.Fatalf("GET /api/me/push%s: %d %s", query, response.StatusCode, raw)
		}
		var state pushState
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		return state, string(raw)
	}
	for name, testCase := range map[string]struct {
		query string
		want  pushState
	}{
		"retired key":    {"?device=" + retiredKey, pushState{ThisDevice: true, ThisDeviceStale: true}},
		"current key":    {"?device=" + currentKey, pushState{ThisDevice: true}},
		"unknown device": {"?device=" + unknownKey, pushState{}},
		"no device":      {"", pushState{}},
	} {
		state, raw := read(fixture.server.URL, testCase.query)
		if state != testCase.want {
			t.Fatalf("%s: state is %#v, want %#v", name, state, testCase.want)
		}
		for _, secret := range []string{currentEndpoint, retiredEndpoint, retiredKeyID, fixture.keyID, "key_id"} {
			if strings.Contains(raw, secret) {
				t.Fatalf("%s: the state must not carry %q: %s", name, secret, raw)
			}
		}
	}

	off := newPushTestServer(t, false)
	if state, _ := read(off.server.URL, "?device="+retiredKey); state != (pushState{}) {
		t.Fatalf("a server without web push names no device stale: %#v", state)
	}
}

func TestWebPushKeyIDIsTheLoggedFingerprint(t *testing.T) {
	t.Parallel()
	publicKey, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	if got := webPushKeyID(" " + publicKey + " "); got != webpush.KeyFingerprint(publicKey) || got == "" {
		t.Fatalf("key id is %q, fingerprint %q", got, webpush.KeyFingerprint(publicKey))
	}
	if got := webPushKeyID(" "); got != "" {
		t.Fatalf("no key must name no key, got %q", got)
	}
}

func TestPushEndpointsRejectUnusableSubscriptions(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, true)
	valid := `"user_id":"` + fixture.owner.ID + `","keys":{"p256dh":"` + exampleClientKey + `","auth":"` + exampleClientAuth + `"}`
	who := `"user_id":"` + fixture.owner.ID + `",`
	offCurve := offCurveClientKey(t)
	// Built rather than written out so no credential-shaped literal sits in the source.
	endpointWithUserinfo := (&url.URL{Scheme: "https", User: url.UserPassword("user", "pass"), Host: "push.example.com", Path: "/send/x"}).String()
	for name, body := range map[string]string{
		"loopback endpoint":  `{"endpoint":"https://127.0.0.1/send/x",` + valid + `}`,
		"private endpoint":   `{"endpoint":"https://10.0.0.1/send/x",` + valid + `}`,
		"localhost endpoint": `{"endpoint":"https://localhost/send/x",` + valid + `}`,
		"bare host endpoint": `{"endpoint":"https://push/send/x",` + valid + `}`,
		"plain http":         `{"endpoint":"http://push.example.com/send/x",` + valid + `}`,
		"no endpoint":        `{"endpoint":"",` + valid + `}`,
		"credentials":        `{"endpoint":"` + endpointWithUserinfo + `",` + valid + `}`,
		"off curve key":      `{` + who + `"endpoint":"https://push.example.com/send/x","keys":{"p256dh":"` + offCurve + `","auth":"` + exampleClientAuth + `"}}`,
		"short key":          `{` + who + `"endpoint":"https://push.example.com/send/x","keys":{"p256dh":"AAAA","auth":"` + exampleClientAuth + `"}}`,
		"short auth":         `{` + who + `"endpoint":"https://push.example.com/send/x","keys":{"p256dh":"` + exampleClientKey + `","auth":"AAAA"}}`,
		"no keys":            `{` + who + `"endpoint":"https://push.example.com/send/x"}`,
		"no account":         `{"endpoint":"https://push.example.com/send/x","keys":{"p256dh":"` + exampleClientKey + `","auth":"` + exampleClientAuth + `"}}`,
		"not json":           `{`,
	} {
		t.Run(name, func(t *testing.T) {
			expectStatus(t, http.MethodPut, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(body), http.StatusBadRequest)
		})
	}
	for name, body := range map[string]string{
		"no endpoint": `{` + who + `"endpoint":""}`,
		"no account":  `{"endpoint":"https://push.example.com/send/x"}`,
		"not json":    `{`,
	} {
		t.Run("removal "+name, func(t *testing.T) {
			expectStatus(t, http.MethodDelete, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(body), http.StatusBadRequest)
		})
	}
}

func TestPushEndpointsAreInvisibleWhenTheFeatureIsOff(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, false)
	state := getJSON[struct {
		Enabled       bool                     `json:"enabled"`
		VAPIDKey      string                   `json:"vapid_public_key"`
		Subscriptions []store.PushSubscription `json:"subscriptions"`
	}](t, fixture.server.URL+"/api/me/push")
	if state.Enabled || state.VAPIDKey != "" || state.Subscriptions == nil || len(state.Subscriptions) != 0 {
		t.Fatalf("unexpected disabled state %#v", state)
	}
	body := `{"endpoint":"https://push.example.com/send/x","keys":{"p256dh":"` + exampleClientKey + `","auth":"` + exampleClientAuth + `"}}`
	expectStatus(t, http.MethodPut, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(body), http.StatusNotFound)
	expectStatus(t, http.MethodDelete, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(`{"user_id":"`+fixture.owner.ID+`","endpoint":"https://push.example.com/send/x"}`), http.StatusNotFound)
	subscriptions, err := fixture.store.ListPushSubscriptions(context.Background(), fixture.owner.ID)
	if err != nil || len(subscriptions) != 0 {
		t.Fatalf("a disabled feature must never write a row: %#v %v", subscriptions, err)
	}
}

func TestPushEndpointsRejectBotTokens(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, true)
	ctx := context.Background()
	workspaces, err := fixture.store.ListWorkspaces(ctx, fixture.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, botToken, err := fixture.store.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspaces[0].ID,
		DisplayName: "Pusher",
		CreatedBy:   fixture.owner.ID,
		OwnerUserID: fixture.owner.ID,
		Scopes:      []string{"profile:read", "messages:write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"endpoint":"https://push.example.com/send/x","keys":{"p256dh":"` + exampleClientKey + `","auth":"` + exampleClientAuth + `"}}`
	expectStatusWithBearer(t, botToken.Token, http.MethodPut, fixture.server.URL+"/api/me/push/subscriptions", strings.NewReader(body), http.StatusForbidden)
}

func putPushSubscription(t *testing.T, baseURL, userID, endpoint, label string) struct {
	Subscription store.PushSubscription `json:"subscription"`
} {
	t.Helper()
	body := map[string]any{
		"endpoint":   endpoint,
		"keys":       map[string]string{"p256dh": exampleClientKey, "auth": exampleClientAuth},
		"user_agent": label,
		"user_id":    userID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPut, baseURL+"/api/me/push/subscriptions", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status %d: %s", response.StatusCode, raw)
	}
	var out struct {
		Subscription store.PushSubscription `json:"subscription"`
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// offCurveClientKey is a correctly shaped uncompressed point that is not on
// P-256. Accepting one would hand the delivery worker a key that cannot agree.
func offCurveClientKey(t *testing.T) string {
	t.Helper()
	raw, err := webpush.DecodeKey(exampleClientKey)
	if err != nil {
		t.Fatal(err)
	}
	raw[10] ^= 0xff
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestPushRegistrationNeedsASessionOrTheDevelopmentIdentity(t *testing.T) {
	t.Parallel()
	user := store.User{ID: "usr_1"}
	for name, testCase := range map[string]struct {
		act  actor
		want bool
	}{
		"session":             {actor{user: user, sessionToken: "session"}, true},
		"access session":      {actor{user: user, accessSessionToken: "minted"}, true},
		"development":         {actor{user: user, developmentFallback: true}, true},
		"no session at all":   {actor{user: user}, false},
		"empty access minted": {actor{user: user, accessSessionToken: ""}, false},
	} {
		if got := pushRegistrationAllowed(testCase.act); got != testCase.want {
			t.Fatalf("%s: allowed is %v, want %v", name, got, testCase.want)
		}
	}
}

// TestPushRegistrationFollowsTheCookieSession registers on a production
// server, where the development fallback is off, and signs the session out.
func TestPushRegistrationFollowsTheCookieSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newAccessTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-cookie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := newProductionPushHandler(t, st, AccessConfig{})
	endpoint := "https://push.example.com/send/cookie"
	recorder := servePushPut(handler, owner.ID, endpoint, func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, owner.ID, endpoint, ""); err != nil {
		t.Fatalf("a signed-in device must be deliverable: %v", err)
	}
	if err := st.RevokeSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, owner.ID, endpoint, ""); !errors.Is(err, store.ErrPushSessionEnded) {
		t.Fatalf("signing out must stop the device: %v", err)
	}

	anonymous := servePushPut(handler, owner.ID, endpoint+"-anonymous", func(*http.Request) {})
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("a production server must refuse an anonymous registration: %d", anonymous.Code)
	}
}

// TestPushRegistrationRefusesAnAccountChangedUnderneath is one browser, one
// cookie jar: a tab still showing account A asks to register while the cookie
// now belongs to B, who never turned push on. Nothing is written for either
// account, and the same request naming B goes through.
func TestPushRegistrationRefusesAnAccountChangedUnderneath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newAccessTestStore(t)
	accountA, err := st.EnsureBootstrap(ctx, "Owner", "push-account-a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "push-account-b@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	sessionB, err := st.CreateSession(ctx, accountB.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := newProductionPushHandler(t, st, AccessConfig{})
	asB := func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: "cc_session", Value: sessionB.Token})
	}
	endpoint := "https://push.example.com/send/shared-browser"

	stale := servePushPut(handler, accountA.ID, endpoint, asB)
	if stale.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", stale.Code, stale.Body.String())
	}
	if !strings.Contains(stale.Body.String(), errPushRegistrationAccountChanged.Error()) {
		t.Fatalf("the refusal must say why: %s", stale.Body.String())
	}
	for _, account := range []store.User{accountA, accountB} {
		subscriptions, err := st.ListPushSubscriptions(ctx, account.ID)
		if err != nil || len(subscriptions) != 0 {
			t.Fatalf("a refused registration must write nothing for %s: %#v %v", account.DisplayName, subscriptions, err)
		}
	}

	current := servePushPut(handler, accountB.ID, endpoint, asB)
	if current.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", current.Code, current.Body.String())
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, accountB.ID, endpoint, ""); err != nil {
		t.Fatalf("the signed-in account naming itself must register: %v", err)
	}
}

// TestPushRemovalRefusesAnAccountChangedUnderneath is the same shared cookie
// jar on the way out: a tab still showing A turns push off while the cookie
// belongs to B. B's cookie cannot reach A's row, so the removal is refused
// before anything is touched, and A's device stays listed and deliverable.
// Naming the signed-in account removes it.
func TestPushRemovalRefusesAnAccountChangedUnderneath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newAccessTestStore(t)
	accountA, err := st.EnsureBootstrap(ctx, "Owner", "push-removal-a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "push-removal-b@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	sessionA, err := st.CreateSession(ctx, accountA.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionB, err := st.CreateSession(ctx, accountB.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := newProductionPushHandler(t, st, AccessConfig{})
	asA := func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: "cc_session", Value: sessionA.Token})
	}
	asB := func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: "cc_session", Value: sessionB.Token})
	}
	endpoint := "https://push.example.com/send/shared-browser-off"
	if registered := servePushPut(handler, accountA.ID, endpoint, asA); registered.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", registered.Code, registered.Body.String())
	}

	stale := servePushDelete(handler, accountA.ID, endpoint, asB)
	if stale.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", stale.Code, stale.Body.String())
	}
	if !strings.Contains(stale.Body.String(), errPushRemovalAccountChanged.Error()) {
		t.Fatalf("the refusal must say why: %s", stale.Body.String())
	}
	subscriptions, err := st.ListPushSubscriptions(ctx, accountA.ID)
	if err != nil || len(subscriptions) != 1 {
		t.Fatalf("a refused removal must leave the device listed: %#v %v", subscriptions, err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, accountA.ID, endpoint, ""); err != nil {
		t.Fatalf("a refused removal must leave the device deliverable: %v", err)
	}

	current := servePushDelete(handler, accountA.ID, endpoint, asA)
	if current.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", current.Code, current.Body.String())
	}
	subscriptions, err = st.ListPushSubscriptions(ctx, accountA.ID)
	if err != nil || len(subscriptions) != 0 {
		t.Fatalf("the signed-in account naming itself must remove the device: %#v %v", subscriptions, err)
	}
}

// TestPushRegistrationThroughAccessBindsTheMintedSession is the trusted-proxy
// path: a request carrying only a Cloudflare Access assertion. The session the
// assertion mints is the one the device follows, so signing out of it stops
// delivery the same as for a cookie.
func TestPushRegistrationThroughAccessBindsTheMintedSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key := newAccessTestKey(t)
	jwks, _ := newAccessTestJWKSServer(t, func() map[string]*rsa.PublicKey {
		return map[string]*rsa.PublicKey{"key-1": &key.PublicKey}
	})
	st := newAccessTestStore(t)
	handler := newProductionPushHandler(t, st, AccessConfig{TeamDomain: jwks.URL, Audience: "test-push-aud", HTTPClient: jwks.Client()})
	assertion := signAccessTestToken(t, key, "key-1", jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": jwks.URL, "aud": "test-push-aud",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Minute).Unix(),
		"email": "push-access@example.com",
	})
	// The app reads the signed-in account before it registers, and names it
	// in the registration.
	meRequest := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meRequest.Header.Set(accessAssertionHeader, assertion)
	meRecorder := httptest.NewRecorder()
	handler.ServeHTTP(meRecorder, meRequest)
	var me struct {
		User store.User `json:"user"`
	}
	if err := json.Unmarshal(meRecorder.Body.Bytes(), &me); err != nil || me.User.ID == "" {
		t.Fatalf("GET /api/me through Access: %d %s", meRecorder.Code, meRecorder.Body.String())
	}
	endpoint := "https://push.example.com/send/access"
	recorder := servePushPut(handler, me.User.ID, endpoint, func(request *http.Request) {
		request.Header.Set(accessAssertionHeader, assertion)
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "" {
		t.Fatalf("the assertion must mint a session cookie: %#v", cookies)
	}
	user, err := st.GetSessionUser(ctx, cookies[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, user.ID, endpoint, ""); err != nil {
		t.Fatalf("the device must follow the minted session: %v", err)
	}
	if err := st.RevokeSession(ctx, cookies[0].Value); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, user.ID, endpoint, ""); !errors.Is(err, store.ErrPushSessionEnded) {
		t.Fatalf("signing out of the minted session must stop the device: %v", err)
	}
}

// TestPushRegistrationByTheDevelopmentIdentity keeps local development
// working: the loopback fallback has no session, and its row has none to
// follow.
func TestPushRegistrationByTheDevelopmentIdentity(t *testing.T) {
	t.Parallel()
	fixture := newPushTestServer(t, true)
	endpoint := "https://push.example.com/send/development"
	putPushSubscription(t, fixture.server.URL, fixture.owner.ID, endpoint, "laptop")
	if _, err := fixture.store.GetPushSubscriptionDelivery(context.Background(), fixture.owner.ID, endpoint, ""); err != nil {
		t.Fatalf("a development registration must be deliverable: %v", err)
	}
}

func newProductionPushHandler(t *testing.T, st *sqlitestore.Store, access AccessConfig) http.Handler {
	t.Helper()
	publicKey, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	return New(st, realtime.NewHub(), Options{
		DisableDevAuth:   true,
		Access:           access,
		WebPushNotifier:  &recordingNotifier{},
		WebPushPublicKey: publicKey,
	}).Handler()
}

func servePushPut(handler http.Handler, userID, endpoint string, authenticate func(*http.Request)) *httptest.ResponseRecorder {
	body := `{"user_id":"` + userID + `","endpoint":"` + endpoint + `","keys":{"p256dh":"` + exampleClientKey + `","auth":"` + exampleClientAuth + `"},"user_agent":"phone"}`
	request := httptest.NewRequest(http.MethodPut, "/api/me/push/subscriptions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "1")
	authenticate(request)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func servePushDelete(handler http.Handler, userID, endpoint string, authenticate func(*http.Request)) *httptest.ResponseRecorder {
	body := `{"user_id":"` + userID + `","endpoint":"` + endpoint + `"}`
	request := httptest.NewRequest(http.MethodDelete, "/api/me/push/subscriptions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "1")
	authenticate(request)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
