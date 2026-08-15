package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
