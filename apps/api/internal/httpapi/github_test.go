package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/openclaw/clickclack/apps/api/internal/authpolicy"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestGitHubOAuthDefaultHTTPClientTimeout(t *testing.T) {
	t.Parallel()
	cfg := GitHubOAuthConfig{}.withDefaults()
	if cfg.HTTPClient == nil || cfg.HTTPClient.Timeout != defaultGitHubHTTPTimeout {
		t.Fatalf("expected default timeout %s, got %#v", defaultGitHubHTTPTimeout, cfg.HTTPClient)
	}
	customClient := &http.Client{}
	cfg = GitHubOAuthConfig{HTTPClient: customClient}.withDefaults()
	if cfg.HTTPClient != customClient {
		t.Fatal("expected custom client to be preserved")
	}
}

func TestGitHubDesktopOAuthFlow(t *testing.T) {
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

	tokenRequests := make(chan oauthTokenRequest, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			tokenRequests <- oauthTokenRequest{
				RedirectURL: r.FormValue("redirect_uri"),
				Verifier:    r.FormValue("code_verifier"),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "d-token"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 77, "login": "desktop", "email": "desktop@example.com"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      provider.URL + "/authorize",
		TokenURL:     provider.URL + "/token",
		UserURL:      provider.URL + "/user",
		EmailsURL:    provider.URL + "/emails",
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	verifier := strings.Repeat("v", 43)
	challenge := desktopCodeChallenge(verifier)
	resp, err := client.Get(server.URL + "/api/auth/github/desktop/start?code_challenge=" + challenge + "&desktop_protocol=2")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), provider.URL+"/authorize?") {
		t.Fatalf("unexpected desktop start response: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	state, bindingCookie, authorizationURL := oauthStartResponse(t, resp)
	resp.Body.Close()
	if authorizationURL.Query().Get("code_challenge_method") != "S256" || authorizationURL.Query().Get("code_challenge") == "" {
		t.Fatalf("expected GitHub PKCE challenge, got %s", authorizationURL.String())
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	grantCode := callback.Query().Get("code")
	if resp.StatusCode != http.StatusFound || callback.Scheme != "chat.clickclack.desktop" || callback.Host != "" || callback.Path != "/auth/callback" || !validDesktopCode(grantCode, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		t.Fatalf("unexpected desktop callback: %s %s", resp.Status, callback.String())
	}
	if cookie := findCookie(resp.Cookies(), "cc_session"); cookie != nil {
		t.Fatalf("desktop callback leaked a browser session cookie: %#v", cookie)
	}
	resp.Body.Close()
	tokenRequest := <-tokenRequests
	if tokenRequest.RedirectURL != server.URL+"/api/auth/github/callback" {
		t.Fatalf("unexpected token redirect URI %q", tokenRequest.RedirectURL)
	}
	if desktopCodeChallenge(tokenRequest.Verifier) != authorizationURL.Query().Get("code_challenge") {
		t.Fatal("token exchange verifier did not match the authorization PKCE challenge")
	}

	consumeServer := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(consumeServer.Close)

	consume := func(origin, candidateVerifier string) *http.Response {
		t.Helper()
		body, err := json.Marshal(map[string]string{"code": grantCode, "code_verifier": candidateVerifier})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, consumeServer.URL+"/api/auth/github/desktop/consume", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp = consume("https://evil.example", verifier)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected cross-site consume rejection, got %s", resp.Status)
	}
	resp.Body.Close()
	resp = consume(consumeServer.URL, strings.Repeat("x", 43))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected wrong verifier rejection, got %s", resp.Status)
	}
	resp.Body.Close()
	resp = consume(consumeServer.URL, verifier)
	sessionCookie := findCookie(resp.Cookies(), "cc_session")
	if resp.StatusCode != http.StatusOK || sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected desktop session cookie, got %s %#v", resp.Status, sessionCookie)
	}
	resp.Body.Close()
	resp = consume(consumeServer.URL, verifier)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected one-time grant rejection, got %s", resp.Status)
	}
	resp.Body.Close()
}

func TestLegacyDesktopOAuthFlow(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "legacy-token"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 78, "login": "legacy", "email": "legacy@example.com"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      provider.URL + "/authorize",
		TokenURL:     provider.URL + "/token",
		UserURL:      provider.URL + "/user",
		EmailsURL:    provider.URL + "/emails",
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	verifier := strings.Repeat("l", 43)
	challenge := desktopCodeChallenge(verifier)
	resp, err := client.Get(server.URL + "/api/auth/github/desktop/start?code_challenge=" + challenge)
	if err != nil {
		t.Fatal(err)
	}
	state, bindingCookie, _ := oauthStartResponse(t, resp)
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	grantCode := callback.Query().Get("code")
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || callback.Scheme != "clickclack" || callback.Host != "auth" || callback.Path != "/callback" || len(grantCode) != 32 || !validOAuthGrantCode(grantCode) {
		t.Fatalf("unexpected legacy desktop callback: %s %s", resp.Status, callback.String())
	}

	body, err := json.Marshal(map[string]string{"code": grantCode, "code_verifier": verifier})
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequest(http.MethodPost, server.URL+"/api/auth/github/desktop/consume", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie := findCookie(resp.Cookies(), "cc_session")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("legacy desktop consume did not create a session: %s %#v", resp.Status, sessionCookie)
	}
}

func TestDesktopOAuthProtocolCompatibility(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	challenge := strings.Repeat("a", 43)
	defaultServer := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      "https://github.example/authorize",
	}}).Handler()
	legacyRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/github/desktop/start?code_challenge="+challenge, nil)
	legacyRequest.RemoteAddr = "127.0.0.1:12345"
	legacyRecorder := httptest.NewRecorder()
	defaultServer.ServeHTTP(legacyRecorder, legacyRequest)
	if legacyRecorder.Code != http.StatusFound {
		t.Fatalf("expected legacy desktop protocol on the default cookie server, got %d", legacyRecorder.Code)
	}

	names, err := authpolicy.NewCookieNames("prod", "https://chat.example.com")
	if err != nil {
		t.Fatal(err)
	}
	namespacedAPI := New(st, realtime.NewHub(), Options{
		CookieNames: names,
		GitHubOAuth: GitHubOAuthConfig{
			ClientID:     "client",
			ClientSecret: "secret",
			PublicURL:    "https://chat.example.com",
			AuthURL:      "https://github.example/authorize",
		},
		MetricsEnabled: true,
	})
	namespacedServer := namespacedAPI.Handler()
	oldRequest := httptest.NewRequest(http.MethodGet, "https://chat.example.com/api/auth/github/desktop/start?code_challenge="+challenge, nil)
	oldRecorder := httptest.NewRecorder()
	namespacedServer.ServeHTTP(oldRecorder, oldRequest)
	if oldRecorder.Code != http.StatusUpgradeRequired || oldRecorder.Header().Get("Cache-Control") != "no-store" || !strings.Contains(oldRecorder.Body.String(), "Update ClickClack") {
		t.Fatalf("expected early desktop upgrade response, got %d %q", oldRecorder.Code, oldRecorder.Body.String())
	}
	if oldRecorder.Header().Get("Location") != "" {
		t.Fatalf("old desktop client was redirected to GitHub: %q", oldRecorder.Header().Get("Location"))
	}

	newRequest := httptest.NewRequest(http.MethodGet, "https://chat.example.com/api/auth/github/desktop/start?code_challenge="+challenge+"&desktop_protocol=2", nil)
	newRecorder := httptest.NewRecorder()
	namespacedServer.ServeHTTP(newRecorder, newRequest)
	if newRecorder.Code != http.StatusFound {
		t.Fatalf("expected protocol 2 namespaced start, got %d %s", newRecorder.Code, newRecorder.Body.String())
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "https://chat.example.com/api/auth/github/desktop/start?code_challenge="+challenge+"&desktop_protocol=3", nil)
	invalidRecorder := httptest.NewRecorder()
	namespacedServer.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported protocol rejection, got %d", invalidRecorder.Code)
	}
	metrics := namespacedAPI.metrics.render(buildMetadata{})
	for _, expected := range []string{
		`clickclack_github_oauth_events_total{event="desktop_start"} 3`,
		`clickclack_github_oauth_events_total{event="desktop_upgrade_required"} 1`,
		`clickclack_github_oauth_events_total{event="desktop_protocol_rejected"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("OAuth metrics missing %q:\n%s", expected, metrics)
		}
	}

	legacyGrant, err := newDesktopOAuthGrantCode(1)
	if err != nil {
		t.Fatal(err)
	}
	if !validOAuthGrantCode(legacyGrant) || len(legacyGrant) != 32 {
		t.Fatalf("invalid legacy desktop grant %q", legacyGrant)
	}
	legacyCallback, err := url.Parse(desktopOAuthCallback(1, legacyGrant))
	if err != nil {
		t.Fatal(err)
	}
	if legacyCallback.Scheme != "clickclack" || legacyCallback.Host != "auth" || legacyCallback.Path != "/callback" || legacyCallback.Query().Get("code") != legacyGrant {
		t.Fatalf("unexpected legacy desktop callback %s", legacyCallback)
	}
}

func TestGitHubOAuthFlow(t *testing.T) {
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

	tokenRequests := make(chan oauthTokenRequest, 16)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			tokenRequests <- oauthTokenRequest{
				RedirectURL: r.FormValue("redirect_uri"),
				Verifier:    r.FormValue("code_verifier"),
			}
			switch r.FormValue("code") {
			case "ok":
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gh-token"})
			case "empty":
				_ = json.NewEncoder(w).Encode(map[string]string{})
			case "api-fail":
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "api-fail"})
			case "missing-id":
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "missing-id"})
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/user":
			switch r.Header.Get("Authorization") {
			case "Bearer gh-token":
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "octo", "name": "Octo User", "avatar_url": "https://example.com/a.png"})
			case "Bearer missing-id":
				_ = json.NewEncoder(w).Encode(map[string]any{"login": "missing"})
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
		case "/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"email": "octo@example.com", "primary": true}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      provider.URL + "/authorize",
		TokenURL:     provider.URL + "/token",
		UserURL:      provider.URL + "/user",
		EmailsURL:    provider.URL + "/emails",
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(server.URL + "/api/auth/github/start")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), provider.URL+"/authorize?") {
		t.Fatalf("unexpected start response: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	state, bindingCookie, authorizationURL := oauthStartResponse(t, resp)
	resp.Body.Close()
	if authorizationURL.Query().Get("code_challenge_method") != "S256" || authorizationURL.Query().Get("code_challenge") == "" {
		t.Fatalf("expected GitHub PKCE challenge, got %s", authorizationURL.String())
	}

	wrongBindingRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongBindingRequest.AddCookie(&http.Cookie{Name: bindingCookie.Name, Value: strings.Repeat("x", oauthEncodedSecretLength)})
	wrongBindingResponse, err := client.Do(wrongBindingRequest)
	if err != nil {
		t.Fatal(err)
	}
	wrongBindingResponse.Body.Close()
	if wrongBindingResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected wrong browser binding rejection, got %s", wrongBindingResponse.Status)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+state, nil)
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
	if tokenRequest.RedirectURL != server.URL+"/api/auth/github/callback" {
		t.Fatalf("unexpected token redirect URI %q", tokenRequest.RedirectURL)
	}
	if desktopCodeChallenge(tokenRequest.Verifier) != authorizationURL.Query().Get("code_challenge") {
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
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/workspaces", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var openWorkspaces struct {
		Workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
			Role string `json:"role"`
		} `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openWorkspaces); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(openWorkspaces.Workspaces) != 1 || openWorkspaces.Workspaces[0].Name != "Guests" || openWorkspaces.Workspaces[0].Slug != "guests" {
		t.Fatalf("expected open github login to join guest workspace, got %s %#v", resp.Status, openWorkspaces.Workspaces)
	}
	if openWorkspaces.Workspaces[0].Role != store.WorkspaceRoleMember {
		t.Fatalf("expected open github login without moderator org to be a member, got %#v", openWorkspaces.Workspaces[0])
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/workspaces/"+openWorkspaces.Workspaces[0].ID+"/channels", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var guestChannels struct {
		Channels []struct {
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&guestChannels); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(guestChannels.Channels) < 2 {
		t.Fatalf("expected open github login without moderator org to see guest workspace rooms, got %s %#v", resp.Status, guestChannels.Channels)
	}

	for _, tc := range []struct {
		name string
		code string
		want int
	}{
		{"missing code", "", http.StatusBadRequest},
		{"token status", "bad", http.StatusBadGateway},
		{"missing token", "empty", http.StatusBadGateway},
		{"api failure", "api-fail", http.StatusBadGateway},
		{"missing profile id", "missing-id", http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			startResp, err := client.Get(server.URL + "/api/auth/github/start")
			if err != nil {
				t.Fatal(err)
			}
			testState, testBinding, _ := oauthStartResponse(t, startResp)
			startResp.Body.Close()
			path := server.URL + "/api/auth/github/callback?state=" + testState
			if tc.code != "" {
				path += "&code=" + tc.code
			}
			req, err := http.NewRequest(http.MethodGet, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(testBinding)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("expected %d, got %s", tc.want, resp.Status)
			}
		})
	}

	if got := firstNonEmpty("", "  ", "value"); got != "value" {
		t.Fatalf("unexpected first non-empty value %q", got)
	}
	if got := firstNonEmpty("", " "); got != "" {
		t.Fatalf("expected empty fallback, got %q", got)
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/anything", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{PublicURL: "https://public.example"}})
	if got, err := srv.githubRedirectURL(req); err != nil || got != "https://public.example/api/auth/github/callback" {
		t.Fatalf("unexpected public redirect url %q", got)
	}
}

func TestNamespacedOAuthSessionsCoexistAcrossSameHostPorts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "shared-token"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 91, "login": "shared", "email": "shared@example.com"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)

	newInstance := func(namespace string) (*httptest.Server, *sqlitestore.Store, authpolicy.CookieNames) {
		t.Helper()
		st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), namespace+".db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		if err := st.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		var handler http.Handler
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(server.Close)
		names, err := authpolicy.NewCookieNames(namespace, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		handler = New(st, realtime.NewHub(), Options{
			CookieNames:    names,
			DisableDevAuth: true,
			GitHubOAuth: GitHubOAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				PublicURL:    server.URL,
				AuthURL:      provider.URL + "/authorize",
				TokenURL:     provider.URL + "/token",
				UserURL:      provider.URL + "/user",
				EmailsURL:    provider.URL + "/emails",
			},
		}).Handler()
		return server, st, names
	}

	first, _, firstNames := newInstance("first")
	second, _, secondNames := newInstance("second")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		Jar:           jar,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	signIn := func(server *httptest.Server, names authpolicy.CookieNames) {
		t.Helper()
		resp, err := client.Get(server.URL + "/api/auth/github/start")
		if err != nil {
			t.Fatal(err)
		}
		state, _, _ := oauthStartResponseWithCookieName(t, resp, names.OAuthBinding)
		resp.Body.Close()
		resp, err = client.Get(server.URL + "/api/auth/github/callback?code=ok&state=" + state)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || findCookie(resp.Cookies(), names.Session) == nil {
			t.Fatalf("expected namespaced session from %s, got %s %#v", server.URL, resp.Status, resp.Cookies())
		}
	}
	signIn(first, firstNames)
	signIn(second, secondNames)

	for _, server := range []*httptest.Server{first, second} {
		resp, err := client.Get(server.URL + "/api/me")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected independent session at %s, got %s", server.URL, resp.Status)
		}
	}
	firstURL, _ := url.Parse(first.URL)
	cookies := jar.Cookies(firstURL)
	if findCookie(cookies, firstNames.Session) == nil || findCookie(cookies, secondNames.Session) == nil {
		t.Fatalf("expected both port-shared cookies to coexist, got %#v", cookies)
	}
}

func TestGitHubOAuthAllowsConcurrentStartsForOneBrowser(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": r.FormValue("code")})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 92, "login": "concurrent", "email": "concurrent@example.com"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      provider.URL + "/authorize",
		TokenURL:     provider.URL + "/token",
		UserURL:      provider.URL + "/user",
		EmailsURL:    provider.URL + "/emails",
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	firstResponse, err := client.Get(server.URL + "/api/auth/github/start")
	if err != nil {
		t.Fatal(err)
	}
	firstState, bindingCookie, _ := oauthStartResponse(t, firstResponse)
	firstResponse.Body.Close()

	secondRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest.AddCookie(bindingCookie)
	secondResponse, err := client.Do(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondURL, err := url.Parse(secondResponse.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	secondState := secondURL.Query().Get("state")
	secondResponse.Body.Close()
	if !validDesktopCode(secondState, oauthEncodedSecretLength, oauthEncodedSecretLength) || secondState == firstState {
		t.Fatalf("unexpected concurrent state values %q and %q", firstState, secondState)
	}

	for _, state := range []string{firstState, secondState} {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code="+state+"&state="+state, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(bindingCookie)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("expected concurrent callback success, got %s", resp.Status)
		}
	}
}

func TestGitHubOAuthModeratorOrgJoinsGuestWorkspaceAsModerator(t *testing.T) {
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

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "mod-token"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 43, "login": "mod", "email": "mod@example.com"})
		case "/memberships/orgs/openclaw":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "active", "organization": map[string]any{"login": "openclaw"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:      "client",
		ClientSecret:  "secret",
		AuthURL:       provider.URL + "/authorize",
		TokenURL:      provider.URL + "/token",
		UserURL:       provider.URL + "/user",
		EmailsURL:     provider.URL + "/emails",
		MembershipURL: provider.URL + "/memberships/orgs/",
		ModeratorOrg:  "openclaw",
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(server.URL + "/api/auth/github/start")
	if err != nil {
		t.Fatal(err)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if scope := location.Query().Get("scope"); scope != "read:user user:email read:org" {
		t.Fatalf("unexpected scope %q", scope)
	}
	state, bindingCookie, _ := oauthStartResponse(t, resp)
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie := findCookie(resp.Cookies(), "cc_session")
	resp.Body.Close()
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/workspaces", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var workspaces struct {
		Workspaces []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
			Slug string `json:"slug"`
		} `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(workspaces.Workspaces) != 1 || workspaces.Workspaces[0].Slug != "guests" || workspaces.Workspaces[0].Role != store.WorkspaceRoleModerator {
		t.Fatalf("expected moderator guest workspace membership, got %s %#v", resp.Status, workspaces.Workspaces)
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/workspaces/"+workspaces.Workspaces[0].ID+"/channels", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var channels struct {
		Channels []struct {
			Name string `json:"name"`
		} `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(channels.Channels) < 2 {
		t.Fatalf("expected moderator to see guest and approved rooms, got %#v", channels.Channels)
	}
}

func TestGitHubOAuthAllowedOrgStillMapsModeratorOrg(t *testing.T) {
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
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Mod", Email: "allowed-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memberships/orgs/openclaw" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "active", "organization": map[string]any{"login": "openclaw"}})
	}))
	t.Cleanup(provider.Close)

	srv := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		MembershipURL: provider.URL + "/memberships/orgs/",
		AllowedOrg:    "openclaw",
		ModeratorOrg:  "openclaw",
	}})
	workspace, err := srv.ensureGitHubWorkspace(ctx, "active", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Slug != "guests" || workspace.Role != store.WorkspaceRoleModerator {
		t.Fatalf("expected allowed moderator to join guest workspace as moderator, got %#v", workspace)
	}
}

func TestGitHubOAuthAllowedOrg(t *testing.T) {
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

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			switch r.FormValue("code") {
			case "member":
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "member-token"})
			case "denied":
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "denied-token"})
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "octo", "email": "octo@example.com"})
		case "/memberships/orgs/openclaw":
			if r.Header.Get("Authorization") == "Bearer denied-token" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":        "active",
				"organization": map[string]any{"login": "OpenClaw"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:      "client",
		ClientSecret:  "secret",
		AuthURL:       provider.URL + "/authorize",
		TokenURL:      provider.URL + "/token",
		UserURL:       provider.URL + "/user",
		EmailsURL:     provider.URL + "/emails",
		MembershipURL: provider.URL + "/memberships/orgs/",
		AllowedOrg:    "openclaw",
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(server.URL + "/api/auth/github/start")
	if err != nil {
		t.Fatal(err)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if scope := location.Query().Get("scope"); scope != "read:user user:email read:org" {
		t.Fatalf("unexpected scope %q", scope)
	}
	state, bindingCookie, _ := oauthStartResponse(t, resp)
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=member&state="+state, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(bindingCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected member callback redirect, got %s", resp.Status)
	}
	sessionCookie := findCookie(resp.Cookies(), "cc_session")
	resp.Body.Close()
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/workspaces", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var workspaces struct {
		Workspaces []struct {
			Name string `json:"name"`
		} `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(workspaces.Workspaces) != 1 || workspaces.Workspaces[0].Name != "ClickClack" {
		t.Fatalf("expected default workspace membership, got %s %#v", resp.Status, workspaces.Workspaces)
	}

	resp, err = client.Get(server.URL + "/api/auth/github/start")
	if err != nil {
		t.Fatal(err)
	}
	deniedState, deniedBinding, _ := oauthStartResponse(t, resp)
	resp.Body.Close()
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=denied&state="+deniedState, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(deniedBinding)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected org denied, got %s", resp.Status)
	}
}

func TestRequestLoggerOmitsQueryString(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	formatter := &pathOnlyLogFormatter{Logger: log.New(&logs, "", 0)}
	router := chi.NewRouter()
	router.Use(middleware.RequestLogger(formatter))
	router.Get("/api/auth/github/callback", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound)
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.test/api/auth/github/callback?code=secret-code&state=secret-state&after_cursor=secret-cursor", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	got := logs.String()
	if strings.Contains(got, "secret-code") || strings.Contains(got, "secret-state") || strings.Contains(got, "secret-cursor") || strings.Contains(got, "?") {
		t.Fatalf("expected query string to be omitted from request log, got %q", got)
	}
	if !strings.Contains(got, `route="/api/auth/github/callback"`) {
		t.Fatalf("expected route pattern in request log, got %q", got)
	}
}

func TestGitHubOrgMembershipChecks(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	responses := map[string]func(http.ResponseWriter){
		"active": func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "active", "organization": map[string]any{"login": "openclaw"}})
		},
		"inactive": func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending", "organization": map[string]any{"login": "openclaw"}})
		},
		"wrong-org": func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "active", "organization": map[string]any{"login": "other"}})
		},
		"broken-json": func(w http.ResponseWriter) {
			_, _ = w.Write([]byte(`{`))
		},
		"server-error": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := responses[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if handler == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		handler(w)
	}))
	t.Cleanup(provider.Close)

	srv := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		MembershipURL: provider.URL + "/memberships/orgs/",
		AllowedOrg:    "openclaw",
	}})
	if err := srv.ensureGitHubAllowedOrgMembership(context.Background(), "active"); err != nil {
		t.Fatalf("expected active membership, got %v", err)
	}
	noOrg := New(st, realtime.NewHub(), Options{})
	if err := noOrg.ensureGitHubAllowedOrgMembership(context.Background(), "token"); err != nil {
		t.Fatalf("expected empty org to skip check, got %v", err)
	}

	for _, tc := range []struct {
		name       string
		token      string
		wantDenied bool
	}{
		{"inactive", "inactive", true},
		{"wrong org", "wrong-org", true},
		{"missing", "missing", true},
		{"broken json", "broken-json", false},
		{"server error", "server-error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
				MembershipURL: provider.URL + "/memberships/orgs/",
				AllowedOrg:    "openclaw",
			}})
			err := srv.ensureGitHubAllowedOrgMembership(context.Background(), tc.token)
			if err == nil {
				t.Fatal("expected membership error")
			}
			if errors.Is(err, errGitHubOrgDenied) != tc.wantDenied {
				t.Fatalf("unexpected denied error state for %v", err)
			}
		})
	}

	badURL := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{MembershipURL: "://bad", AllowedOrg: "openclaw"}})
	if err := badURL.ensureGitHubAllowedOrgMembership(context.Background(), "token"); err == nil {
		t.Fatal("expected bad membership url error")
	}
}

func TestGitHubOAuthErrors(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	expectStatus(t, http.MethodGet, server.URL+"/api/auth/github/start", nil, http.StatusNotImplemented)
	expectStatus(t, http.MethodGet, server.URL+"/api/auth/github/callback?code=x&state=bad", nil, http.StatusBadRequest)

	duplicateBindingServer := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "c",
		ClientSecret: "s",
		AuthURL:      "https://github.example/authorize",
	}}).Handler()
	duplicateBindingRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/github/start", nil)
	duplicateBindingRequest.RemoteAddr = "127.0.0.1:12345"
	duplicateBindingRequest.AddCookie(&http.Cookie{Name: "cc_oauth_binding", Value: strings.Repeat("a", oauthEncodedSecretLength)})
	duplicateBindingRequest.AddCookie(&http.Cookie{Name: "cc_oauth_binding", Value: strings.Repeat("b", oauthEncodedSecretLength)})
	duplicateBindingRecorder := httptest.NewRecorder()
	duplicateBindingServer.ServeHTTP(duplicateBindingRecorder, duplicateBindingRequest)
	if duplicateBindingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate OAuth binding rejection, got %d", duplicateBindingRecorder.Code)
	}

	srv := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{ClientID: "c", ClientSecret: "s", PublicURL: "https://example.test", TokenURL: "://bad", UserURL: "://bad"}})
	req := httptest.NewRequest(http.MethodGet, "https://example.test/callback", nil)
	req.TLS = &tls.ConnectionState{}
	redirectURL, err := srv.githubRedirectURL(req)
	if err != nil || redirectURL != "https://example.test/api/auth/github/callback" {
		t.Fatalf("unexpected tls redirect %q: %v", redirectURL, err)
	}
	if _, err := srv.exchangeGitHubCode(context.Background(), "x", strings.Repeat("v", 43), redirectURL); err == nil {
		t.Fatal("expected bad token url error")
	}
	if err := srv.githubGetJSON(context.Background(), "://bad", "token", &struct{}{}); err == nil {
		t.Fatal("expected bad github api url error")
	}
}

func TestGitHubOAuthDoesNotExposeInternalStoreErrors(t *testing.T) {
	t.Parallel()
	base := newEmptyHTTPStore(t)
	handler := New(failingOAuthTransactionStore{
		Store: base,
		err:   errors.New(`postgres://admin:secret@database.internal:5432/clickclack`),
	}, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      "https://github.example/authorize",
	}}).Handler()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/auth/github/start", nil)
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
	if !strings.Contains(body, "github oauth request failed") {
		t.Fatalf("unexpected public OAuth error: %s", body)
	}
}

type failingOAuthTransactionStore struct {
	store.Store
	err error
}

func (s failingOAuthTransactionStore) CreateOAuthTransaction(context.Context, store.OAuthTransaction) error {
	return s.err
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

type oauthTokenRequest struct {
	RedirectURL string
	Verifier    string
}

func oauthStartResponse(t *testing.T, response *http.Response) (string, *http.Cookie, *url.URL) {
	return oauthStartResponseWithCookieName(t, response, "cc_oauth_binding")
}

func oauthStartResponseWithCookieName(t *testing.T, response *http.Response, bindingCookieName string) (string, *http.Cookie, *url.URL) {
	t.Helper()
	authorizationURL, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorizationURL.Query().Get("state")
	if !validDesktopCode(state, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		t.Fatalf("invalid OAuth state in authorization URL: %q", state)
	}
	bindingCookie := findCookie(response.Cookies(), bindingCookieName)
	if bindingCookie == nil || !validDesktopCode(bindingCookie.Value, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		t.Fatalf("missing OAuth browser binding cookie: %#v", bindingCookie)
	}
	if !bindingCookie.HttpOnly || bindingCookie.Path != "/" || bindingCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe OAuth browser binding cookie: %#v", bindingCookie)
	}
	return state, bindingCookie, authorizationURL
}
