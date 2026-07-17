package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSplitOriginCORS(t *testing.T) {
	t.Parallel()
	handler := New(nil, nil, Options{
		DisableDevAuth: true,
		FrontendURL:    "https://chat.example.com",
		PublicAPIURL:   "https://api.example.com/services/clickclack",
	}).Handler()
	withoutOrigin := httptest.NewRecorder()
	handler.ServeHTTP(withoutOrigin, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if !strings.Contains(withoutOrigin.Header().Get("Vary"), "Origin") {
		t.Fatalf("expected all API variants to vary by origin, got %#v", withoutOrigin.Header())
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/me", nil)
	preflight.Header.Set("Origin", "https://chat.example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	preflight.Header.Set("Access-Control-Request-Headers", "X-ClickClack-CSRF, Content-Type")
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent {
		t.Fatalf("allowed preflight: got %d: %s", preflightResponse.Code, preflightResponse.Body.String())
	}
	if got := preflightResponse.Header().Get("Access-Control-Allow-Origin"); got != "https://chat.example.com" {
		t.Fatalf("unexpected allow origin %q", got)
	}
	if got := preflightResponse.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("unexpected allow credentials %q", got)
	}
	if got := preflightResponse.Header().Get("Access-Control-Allow-Headers"); got != "content-type, x-clickclack-csrf" {
		t.Fatalf("unexpected allow headers %q", got)
	}

	actual := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	actual.Header.Set("Origin", "https://chat.example.com")
	actualResponse := httptest.NewRecorder()
	handler.ServeHTTP(actualResponse, actual)
	if actualResponse.Code != http.StatusUnauthorized || actualResponse.Header().Get("Access-Control-Allow-Origin") != "https://chat.example.com" {
		t.Fatalf("expected credentialed CORS headers on API response, got %d %#v", actualResponse.Code, actualResponse.Header())
	}

	for name, mutate := range map[string]func(*http.Request){
		"origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"method": func(r *http.Request) { r.Header.Set("Access-Control-Request-Method", "CONNECT") },
		"header": func(r *http.Request) { r.Header.Set("Access-Control-Request-Headers", "X-ClickClack-CSRF, X-Evil") },
	} {
		t.Run(name, func(t *testing.T) {
			req := preflight.Clone(preflight.Context())
			req.Header = preflight.Header.Clone()
			mutate(req)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusForbidden || response.Header().Get("Access-Control-Allow-Origin") == "https://evil.example" {
				t.Fatalf("expected rejected preflight, got %d %#v", response.Code, response.Header())
			}
		})
	}
}

func TestSplitOriginTrustAndRuntimeConfig(t *testing.T) {
	t.Parallel()
	server := New(nil, nil, Options{
		FrontendURL:  "http://127.0.0.1:18080",
		PublicAPIURL: "http://127.0.0.1:18081/services/clickclack",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/consume", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://127.0.0.1:18080")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	if !server.sameOriginBrowserRequest(req) {
		t.Fatal("expected exact configured frontend origin to pass browser trust validation")
	}
	req.Header.Set("Origin", "http://127.0.0.1:18082")
	if server.sameOriginBrowserRequest(req) {
		t.Fatal("expected unconfigured frontend origin to fail browser trust validation")
	}

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !strings.Contains(string(body), `window.__CLICKCLACK_CONFIG__={"apiBaseUrl":"http://127.0.0.1:18081/services/clickclack"}`) {
		t.Fatalf("expected injected API base, got %d %q", response.Code, body)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected runtime HTML to avoid shared caching, got %#v", response.Header())
	}
}

func TestPathMountedCookiesUseAPIBasePath(t *testing.T) {
	t.Parallel()
	server := New(nil, nil, Options{PublicAPIURL: "https://api.example.com/services/clickclack"})
	request := httptest.NewRequest(http.MethodGet, "https://api.example.com/services/clickclack/api/me", nil)

	sessionResponse := httptest.NewRecorder()
	server.setSessionCookie(sessionResponse, request, store.Session{
		Token:     strings.Repeat("t", 16),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
	})
	sessionCookie := findCookie(sessionResponse.Result().Cookies(), server.cookies.Session)
	if sessionCookie == nil || sessionCookie.Path != "/services/clickclack" {
		t.Fatalf("expected path-scoped session cookie, got %#v", sessionCookie)
	}

	bindingResponse := httptest.NewRecorder()
	if _, err := server.oauthBrowserBinding(bindingResponse, request); err != nil {
		t.Fatal(err)
	}
	bindingCookie := findCookie(bindingResponse.Result().Cookies(), server.cookies.OAuthBinding)
	if bindingCookie == nil || bindingCookie.Path != "/services/clickclack" {
		t.Fatalf("expected path-scoped OAuth binding cookie, got %#v", bindingCookie)
	}
}

func TestConfiguredCookieSameSite(t *testing.T) {
	t.Parallel()
	if got := configuredCookieSameSite("http://localhost:8080", "http://localhost:8081"); got != http.SameSiteLaxMode {
		t.Fatalf("same-host split should stay same-site, got %v", got)
	}
	if got := configuredCookieSameSite("https://chat.example.com", "https://api.example.com"); got != http.SameSiteNoneMode {
		t.Fatalf("cross-host split should allow credentialed CORS, got %v", got)
	}
}

func TestGitHubCallbackUsesCanonicalAPIBase(t *testing.T) {
	t.Parallel()
	server := New(nil, nil, Options{
		FrontendURL:  "https://chat.example.com",
		PublicAPIURL: "https://api.example.com/services/clickclack",
		GitHubOAuth:  GitHubOAuthConfig{PublicURL: "https://chat.example.com"},
	})
	got, err := server.githubRedirectURL(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/services/clickclack/api/auth/github/callback" {
		t.Fatalf("unexpected GitHub callback URL %q", got)
	}
}
