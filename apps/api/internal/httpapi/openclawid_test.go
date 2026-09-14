package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

type openclawTokenRequest struct {
	RedirectURL   string
	Verifier      string
	Authorization string
	GrantType     string
}

func newOpenClawIDToken(t *testing.T, issuer, audience, email, name string, verified bool, expiresAt time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":            issuer,
		"aud":            audience,
		"sub":            "ocid-user-1",
		"email":          email,
		"email_verified": verified,
		"name":           name,
		"exp":            expiresAt.Unix(),
		"iat":            time.Now().Unix(),
	})
	signed, err := token.SignedString([]byte("test-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestOpenClawIDOAuthFlow(t *testing.T) {
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

	tokenRequests := make(chan openclawTokenRequest, 16)
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			tokenRequests <- openclawTokenRequest{
				RedirectURL:   r.FormValue("redirect_uri"),
				Verifier:      r.FormValue("code_verifier"),
				Authorization: r.Header.Get("Authorization"),
				GrantType:     r.FormValue("grant_type"),
			}
			switch r.FormValue("code") {
			case "ok":
				idToken := newOpenClawIDToken(t, provider.URL, "client", "Crab@Example.com", "Crab User", true, time.Now().Add(5*time.Minute))
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oc-token", "id_token": idToken})
			case "unverified":
				idToken := newOpenClawIDToken(t, provider.URL, "client", "crab@example.com", "Crab User", false, time.Now().Add(5*time.Minute))
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oc-token", "id_token": idToken})
			case "wrong-issuer":
				idToken := newOpenClawIDToken(t, "https://evil.example.com", "client", "crab@example.com", "Crab User", true, time.Now().Add(5*time.Minute))
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oc-token", "id_token": idToken})
			case "expired":
				idToken := newOpenClawIDToken(t, provider.URL, "client", "crab@example.com", "Crab User", true, time.Now().Add(-5*time.Minute))
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oc-token", "id_token": idToken})
			case "missing-id-token":
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oc-token"})
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{OpenClawID: OpenClawIDConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		Issuer:       provider.URL,
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(server.URL + "/api/auth/openclaw/start")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), provider.URL+"/oauth2/authorize?") {
		t.Fatalf("unexpected start response: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	state, bindingCookie, authorizationURL := oauthStartResponse(t, resp)
	resp.Body.Close()
	query := authorizationURL.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatalf("expected PKCE challenge, got %s", authorizationURL.String())
	}
	if query.Get("response_type") != "code" || query.Get("scope") != "openid profile email" || query.Get("client_id") != "client" {
		t.Fatalf("unexpected authorization parameters: %s", authorizationURL.String())
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/openclaw/callback?code=ok&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("unexpected callback response: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "cc_session" {
			sessionCookie = cookie
		}
	}
	resp.Body.Close()
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("expected session cookie")
	}
	tokenRequest := <-tokenRequests
	if tokenRequest.RedirectURL != server.URL+"/api/auth/openclaw/callback" {
		t.Fatalf("unexpected token redirect URI %q", tokenRequest.RedirectURL)
	}
	if tokenRequest.GrantType != "authorization_code" {
		t.Fatalf("unexpected grant type %q", tokenRequest.GrantType)
	}
	if !strings.HasPrefix(tokenRequest.Authorization, "Basic ") {
		t.Fatalf("expected client_secret_basic token authentication, got %q", tokenRequest.Authorization)
	}
	if desktopCodeChallenge(tokenRequest.Verifier) != query.Get("code_challenge") {
		t.Fatal("token exchange verifier did not match the authorization PKCE challenge")
	}

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected session auth, got %s", resp.Status)
	}
	var me struct {
		User struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if me.User.DisplayName != "Crab User" {
		t.Fatalf("unexpected display name %q", me.User.DisplayName)
	}

	// The email is normalized to lowercase, so a magic-link user with the
	// same email links to the same account.
	user, err := st.GetOrCreateUserByEmail(ctx, "magic", "crab@example.com", "Other Name")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != me.User.ID {
		t.Fatalf("expected email-linked user %q, got %q", me.User.ID, user.ID)
	}

	// Replay of the consumed state is rejected.
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/auth/openclaw/callback?code=ok&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected replayed state rejection, got %s", resp.Status)
	}

	// Invalid identity tokens are rejected after the exchange.
	for _, code := range []string{"unverified", "wrong-issuer", "expired"} {
		resp, err = client.Get(server.URL + "/api/auth/openclaw/start")
		if err != nil {
			t.Fatal(err)
		}
		state, bindingCookie, _ = oauthStartResponse(t, resp)
		resp.Body.Close()
		req, err = http.NewRequest(http.MethodGet, server.URL+"/api/auth/openclaw/callback?code="+code+"&state="+state, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(bindingCookie)
		resp, err = client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected %q rejection with 403, got %s", code, resp.Status)
		}
		<-tokenRequests
	}

	// A token response without an id_token is a provider failure.
	resp, err = client.Get(server.URL + "/api/auth/openclaw/start")
	if err != nil {
		t.Fatal(err)
	}
	state, bindingCookie, _ = oauthStartResponse(t, resp)
	resp.Body.Close()
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/auth/openclaw/callback?code=missing-id-token&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected missing id_token to fail with 502, got %s", resp.Status)
	}
	<-tokenRequests
}

func TestOpenClawIDOAuthErrors(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/api/auth/openclaw/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected unconfigured start to return 501, got %s", resp.Status)
	}

	resp, err = http.Get(server.URL + "/api/auth/openclaw/callback?code=ok&state=short")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected malformed state to return 400, got %s", resp.Status)
	}
}

func TestOpenClawIDConfigDefaults(t *testing.T) {
	t.Parallel()
	config := OpenClawIDConfig{ClientID: " client ", ClientSecret: " secret "}.withDefaults()
	if config.Issuer != "https://id.openclaw.ai/api/auth" {
		t.Fatalf("unexpected default issuer %q", config.Issuer)
	}
	if config.AuthURL != "https://id.openclaw.ai/api/auth/oauth2/authorize" {
		t.Fatalf("unexpected default auth URL %q", config.AuthURL)
	}
	if config.TokenURL != "https://id.openclaw.ai/api/auth/oauth2/token" {
		t.Fatalf("unexpected default token URL %q", config.TokenURL)
	}
	if config.ClientID != "client" || config.ClientSecret != "secret" {
		t.Fatal("expected trimmed client credentials")
	}
	if config.HTTPClient == nil || config.HTTPClient.Timeout != defaultOpenClawIDHTTPTimeout {
		t.Fatal("expected default HTTP client timeout")
	}
}

func TestOpenClawIDOAuthDoesNotExposeInternalStoreErrors(t *testing.T) {
	t.Parallel()
	base := newEmptyHTTPStore(t)
	handler := New(failingOAuthTransactionStore{
		Store: base,
		err:   errors.New(`postgres://admin:secret@database.internal:5432/clickclack`),
	}, realtime.NewHub(), Options{OpenClawID: OpenClawIDConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		PublicURL:    "https://app.clickclack.test",
	}}).Handler()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/openclaw/start", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected internal error, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "admin") || strings.Contains(body, "secret") || strings.Contains(body, "database.internal") {
		t.Fatalf("internal OAuth error leaked to client: %s", body)
	}
	if !strings.Contains(body, "openclaw id oauth request failed") {
		t.Fatalf("unexpected public OAuth error: %s", body)
	}
}

func TestOpenClawIDOAuthErrorBranches(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	configured := New(st, realtime.NewHub(), Options{OpenClawID: OpenClawIDConfig{
		ClientID:     "c",
		ClientSecret: "s",
		PublicURL:    "https://app.clickclack.test",
	}}).Handler()

	// Ambiguous browser-binding cookie on start.
	ambiguous := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/openclaw/start", nil)
	ambiguous.RemoteAddr = "127.0.0.1:12345"
	ambiguous.AddCookie(&http.Cookie{Name: "cc_oauth_binding", Value: strings.Repeat("a", oauthEncodedSecretLength)})
	ambiguous.AddCookie(&http.Cookie{Name: "cc_oauth_binding", Value: strings.Repeat("b", oauthEncodedSecretLength)})
	recorder := httptest.NewRecorder()
	configured.ServeHTTP(recorder, ambiguous)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected ambiguous binding rejection, got %d", recorder.Code)
	}

	// Callback with valid-shaped state but no binding cookie.
	missingCookie := httptest.NewRequest(http.MethodGet,
		"http://127.0.0.1:8080/api/auth/openclaw/callback?code=x&state="+strings.Repeat("a", oauthEncodedSecretLength), nil)
	missingCookie.RemoteAddr = "127.0.0.1:12345"
	recorder = httptest.NewRecorder()
	configured.ServeHTTP(recorder, missingCookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected missing binding cookie rejection, got %d", recorder.Code)
	}

	// Start without any public URL outside loopback fails closed.
	noPublic := New(st, realtime.NewHub(), Options{DisableDevAuth: true, OpenClawID: OpenClawIDConfig{
		ClientID:     "c",
		ClientSecret: "s",
	}})
	external := httptest.NewRequest(http.MethodGet, "http://example.test/api/auth/openclaw/start", nil)
	external.RemoteAddr = "203.0.113.9:443"
	if _, err := noPublic.openclawIDRedirectURL(external); err == nil {
		t.Fatal("expected redirect URL error without public URL")
	}

	// Loopback dev fallback derives scheme from TLS state.
	devSrv := New(st, realtime.NewHub(), Options{OpenClawID: OpenClawIDConfig{ClientID: "c", ClientSecret: "s"}})
	loopback := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/openclaw/start", nil)
	loopback.RemoteAddr = "127.0.0.1:9999"
	if url, err := devSrv.openclawIDRedirectURL(loopback); err != nil || !strings.HasPrefix(url, "http://127.0.0.1:8080/") {
		t.Fatalf("unexpected loopback redirect %q: %v", url, err)
	}
	loopbackTLS := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:8443/api/auth/openclaw/start", nil)
	loopbackTLS.RemoteAddr = "127.0.0.1:9999"
	loopbackTLS.TLS = &tls.ConnectionState{}
	if url, err := devSrv.openclawIDRedirectURL(loopbackTLS); err != nil || !strings.HasPrefix(url, "https://") {
		t.Fatalf("unexpected tls redirect %q: %v", url, err)
	}

	// Token exchange against an unparseable token URL surfaces an error.
	badExchange := New(st, realtime.NewHub(), Options{OpenClawID: OpenClawIDConfig{
		ClientID: "c", ClientSecret: "s", PublicURL: "https://example.test", TokenURL: "://bad",
	}})
	if _, err := badExchange.exchangeOpenClawIDCode(context.Background(), "code", strings.Repeat("v", 43), "https://example.test/cb"); err == nil {
		t.Fatal("expected exchange error for bad token URL")
	}
}

type consumeFailingOAuthStore struct {
	store.Store
	err error
}

func (s consumeFailingOAuthStore) ConsumeOAuthTransaction(context.Context, string, string, time.Time) (store.OAuthTransaction, error) {
	return store.OAuthTransaction{}, s.err
}

func TestOpenClawIDOAuthStoreFailureBranches(t *testing.T) {
	t.Parallel()
	base := newEmptyHTTPStore(t)
	config := OpenClawIDConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		PublicURL:    "https://app.clickclack.test",
		AuthURL:      "https://id.example/oauth2/authorize",
	}

	// Capacity exhaustion surfaces 503, not a generic server error.
	capacity := New(failingOAuthTransactionStore{
		Store: base,
		err:   store.ErrOAuthCapacityExceeded,
	}, realtime.NewHub(), Options{OpenClawID: config}).Handler()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/openclaw/start", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	capacity.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected capacity rejection, got %d", recorder.Code)
	}

	// Generic consume failure surfaces the sanitized server error.
	consume := New(consumeFailingOAuthStore{
		Store: base,
		err:   errors.New("disk on fire"),
	}, realtime.NewHub(), Options{OpenClawID: config}).Handler()
	shaped := strings.Repeat("a", oauthEncodedSecretLength)
	callback := httptest.NewRequest(http.MethodGet,
		"http://127.0.0.1:8080/api/auth/openclaw/callback?code=x&state="+shaped, nil)
	callback.RemoteAddr = "127.0.0.1:12345"
	callback.AddCookie(&http.Cookie{Name: "cc_oauth_binding", Value: shaped})
	recorder = httptest.NewRecorder()
	consume.ServeHTTP(recorder, callback)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected consume server error, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "disk on fire") {
		t.Fatalf("internal error leaked: %s", recorder.Body.String())
	}

	// Real start then callback without a code: transaction consumed, 400.
	live := New(base, realtime.NewHub(), Options{OpenClawID: config}).Handler()
	start := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/openclaw/start", nil)
	start.RemoteAddr = "127.0.0.1:12345"
	recorder = httptest.NewRecorder()
	live.ServeHTTP(recorder, start)
	if recorder.Code != http.StatusFound {
		t.Fatalf("expected start redirect, got %d", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	state := location.Query().Get("state")
	binding := findCookie(recorder.Result().Cookies(), "cc_oauth_binding")
	if state == "" || binding == nil {
		t.Fatal("missing state or binding cookie from start")
	}
	noCode := httptest.NewRequest(http.MethodGet,
		"http://127.0.0.1:8080/api/auth/openclaw/callback?state="+url.QueryEscape(state), nil)
	noCode.RemoteAddr = "127.0.0.1:12345"
	noCode.AddCookie(&http.Cookie{Name: "cc_oauth_binding", Value: binding.Value})
	recorder = httptest.NewRecorder()
	live.ServeHTTP(recorder, noCode)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected missing code rejection, got %d", recorder.Code)
	}
}

type failRoundTrip struct{}

func (failRoundTrip) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

func TestOpenClawIDApplyDiscoveryUsesKanidmShapedEndpoints(t *testing.T) {
	t.Parallel()
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/openid/clickclack/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": strings.TrimSuffix(issuer, "/oauth2/openid/clickclack") + "/ui/oauth2",
			"token_endpoint":         strings.TrimSuffix(issuer, "/oauth2/openid/clickclack") + "/oauth2/token",
		})
	}))
	t.Cleanup(provider.Close)
	issuer = provider.URL + "/oauth2/openid/clickclack"
	base := strings.TrimSuffix(provider.URL, "/")
	resolved, err := OpenClawIDConfig{
		ClientID:     "clickclack",
		ClientSecret: "secret",
		Issuer:       issuer,
		HTTPClient:   provider.Client(),
	}.ApplyDiscovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthURL != base+"/ui/oauth2" || resolved.TokenURL != base+"/oauth2/token" {
		t.Fatalf("unexpected discovered endpoints auth=%q token=%q", resolved.AuthURL, resolved.TokenURL)
	}
	st := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{OpenClawID: resolved}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(server.URL + "/api/auth/openclaw/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), base+"/ui/oauth2?") {
		t.Fatalf("expected Kanidm authorize redirect, got %s %s", resp.Status, resp.Header.Get("Location"))
	}
}

func TestOpenClawIDApplyDiscoveryCustomIssuerRequiresDocument(t *testing.T) {
	t.Parallel()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(provider.Close)
	_, err := OpenClawIDConfig{
		ClientID:     "clickclack",
		ClientSecret: "secret",
		Issuer:       provider.URL + "/oauth2/openid/clickclack",
		HTTPClient:   provider.Client(),
	}.ApplyDiscovery(context.Background())
	if err == nil {
		t.Fatal("expected custom issuer without discovery to fail closed")
	}
}

func TestOpenClawIDApplyDiscoveryDefaultIssuerFallsBack(t *testing.T) {
	t.Parallel()
	resolved, err := OpenClawIDConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		HTTPClient:   &http.Client{Timeout: time.Second, Transport: failRoundTrip{}},
	}.ApplyDiscovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthURL != defaultOpenClawIDIssuer+"/oauth2/authorize" || resolved.TokenURL != defaultOpenClawIDIssuer+"/oauth2/token" {
		t.Fatalf("unexpected fallback endpoints auth=%q token=%q", resolved.AuthURL, resolved.TokenURL)
	}
}

func TestOpenClawIDApplyDiscoveryRejectsIssuerMismatch(t *testing.T) {
	t.Parallel()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "https://evil.example.com/oauth2/openid/clickclack",
			"authorization_endpoint": "https://evil.example.com/ui/oauth2",
			"token_endpoint":         "https://evil.example.com/oauth2/token",
		})
	}))
	t.Cleanup(provider.Close)
	_, err := OpenClawIDConfig{
		ClientID:     "clickclack",
		ClientSecret: "secret",
		Issuer:       provider.URL + "/oauth2/openid/clickclack",
		HTTPClient:   provider.Client(),
	}.ApplyDiscovery(context.Background())
	if err == nil {
		t.Fatal("expected discovery issuer mismatch to fail")
	}
}

func TestOpenClawIDApplyDiscoverySkipsFetchWhenEndpointsAreExplicit(t *testing.T) {
	t.Parallel()
	resolved, err := OpenClawIDConfig{
		ClientID:     "clickclack",
		ClientSecret: "secret",
		Issuer:       "https://idm.example.com/oauth2/openid/clickclack",
		AuthURL:      "https://idm.example.com/ui/oauth2",
		TokenURL:     "https://idm.example.com/oauth2/token",
		HTTPClient:   &http.Client{Timeout: time.Second, Transport: failRoundTrip{}},
	}.ApplyDiscovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthURL != "https://idm.example.com/ui/oauth2" || resolved.TokenURL != "https://idm.example.com/oauth2/token" {
		t.Fatalf("explicit endpoints were rewritten: auth=%q token=%q", resolved.AuthURL, resolved.TokenURL)
	}
}

func TestOpenClawIDDiscoveryPreservesEachExplicitEndpoint(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"authorization", "token"} {
		t.Run(field, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(oidcDiscoveryDocument{
					Issuer:                "http://" + r.Host,
					AuthorizationEndpoint: "https://provider.example/authorize",
					TokenEndpoint:         "https://provider.example/token",
				})
			}))
			defer provider.Close()
			cfg := OpenClawIDConfig{ClientID: "client", ClientSecret: "test-secret", Issuer: provider.URL}
			if field == "authorization" {
				cfg.AuthURL = "https://configured.example/authorize"
			} else {
				cfg.TokenURL = "https://configured.example/token"
			}
			got, err := cfg.ApplyDiscovery(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if field == "authorization" && got.AuthURL != cfg.AuthURL {
				t.Fatalf("explicit authorization endpoint overwritten: %s", got.AuthURL)
			}
			if field == "token" && got.TokenURL != cfg.TokenURL {
				t.Fatalf("explicit token endpoint overwritten: %s", got.TokenURL)
			}
		})
	}
}

func TestOpenClawIDDiscoveryRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"invalid json", "{", 200},
		{"oversized body", strings.Repeat(" ", openClawIDDiscoveryMaxBytes+1), 200},
		{"issuer suffix", `{"issuer":"https://id.openclaw.ai/api/auth/","authorization_endpoint":"https://id.openclaw.ai/authorize","token_endpoint":"https://id.openclaw.ai/token"}`, 200},
		{"issuer whitespace", `{"issuer":" https://id.openclaw.ai/api/auth","authorization_endpoint":"https://id.openclaw.ai/authorize","token_endpoint":"https://id.openclaw.ai/token"}`, 200},
		{"insecure endpoint", `{"issuer":"https://id.openclaw.ai/api/auth","authorization_endpoint":"http://remote.example/authorize","token_endpoint":"https://id.openclaw.ai/token"}`, 200},
		{"missing endpoint", `{"issuer":"https://id.openclaw.ai/api/auth"}`, 200},
		{"redirect", "", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := OpenClawIDConfig{ClientID: "client", ClientSecret: "test-secret", HTTPClient: &http.Client{Transport: oidcDiscoveryTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.code, Status: http.StatusText(tc.code), Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: r}, nil
			})}}
			if _, err := cfg.ApplyDiscovery(context.Background()); err == nil {
				t.Fatal("invalid discovery accepted for default issuer")
			}
		})
	}
}

type oidcDiscoveryTransport func(*http.Request) (*http.Response, error)

func (f oidcDiscoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOIDCDiscoveryDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Store(true) }))
	defer target.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer provider.Close()
	_, err := (OpenClawIDConfig{ClientID: "client", ClientSecret: "test-secret", Issuer: provider.URL}).ApplyDiscovery(context.Background())
	if err == nil || followed.Load() {
		t.Fatalf("redirect error=%v followed=%v", err, followed.Load())
	}
}

func TestOpenClawIDDiscoveryPreservesIssuerAndEndpointQueries(t *testing.T) {
	t.Parallel()
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tenant/.well-known/openid-configuration" {
			t.Errorf("discovery path: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(oidcDiscoveryDocument{Issuer: issuer, AuthorizationEndpoint: "https://provider.example/authorize?tenant=one", TokenEndpoint: "https://provider.example/token?tenant=one"})
	}))
	defer provider.Close()
	issuer = provider.URL + "/tenant/"
	got, err := (OpenClawIDConfig{ClientID: "client", ClientSecret: "test-secret", Issuer: issuer}).ApplyDiscovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer != issuer || got.AuthURL != "https://provider.example/authorize?tenant=one" || got.TokenURL != "https://provider.example/token?tenant=one" {
		t.Fatal("issuer or endpoint query changed")
	}
	server := &Server{openclawID: got}
	token := newOpenClawIDToken(t, issuer, "client", "test@example.com", "Test", true, time.Now().Add(time.Hour))
	if _, err := server.validateOpenClawIDToken(token); err != nil {
		t.Fatal(err)
	}
}

func TestOpenClawIDDiscoveryRejectsInvalidExplicitIssuer(t *testing.T) {
	t.Parallel()
	for _, issuer := range []string{"https://id.example/realm?tenant=one", "https://id.example/realm?", "https://id.example/realm#fragment", "http://remote.example/realm", "https://user:password@id.example/realm"} {
		_, err := (OpenClawIDConfig{ClientID: "client", ClientSecret: "test-secret", Issuer: issuer, AuthURL: "https://id.example/authorize", TokenURL: "https://id.example/token", HTTPClient: &http.Client{Transport: failRoundTrip{}}}).ApplyDiscovery(context.Background())
		if err == nil {
			t.Errorf("invalid issuer accepted: %s", issuer)
		}
	}
}
