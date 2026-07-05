package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
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
	resp, err := client.Get(server.URL + "/api/auth/github/desktop/start?code_challenge=" + challenge)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), provider.URL+"/authorize?") {
		t.Fatalf("unexpected desktop start response: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	stateCookie := findCookie(resp.Cookies(), "cc_github_state")
	desktopCookie := findCookie(resp.Cookies(), "cc_github_desktop_state")
	challengeCookie := findCookie(resp.Cookies(), "cc_github_desktop_challenge")
	resp.Body.Close()
	if stateCookie == nil || desktopCookie == nil || desktopCookie.Value != stateCookie.Value || challengeCookie == nil || challengeCookie.Value != challenge {
		t.Fatalf("expected matching desktop state and challenge cookies, got %#v %#v %#v", stateCookie, desktopCookie, challengeCookie)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+stateCookie.Value, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(stateCookie)
	req.AddCookie(desktopCookie)
	req.AddCookie(challengeCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	grantCode := callback.Query().Get("code")
	if resp.StatusCode != http.StatusFound || callback.Scheme != "clickclack" || callback.Host != "auth" || callback.Path != "/callback" || !validDesktopCode(grantCode, 32, 32) {
		t.Fatalf("unexpected desktop callback: %s %s", resp.Status, callback.String())
	}
	if cookie := findCookie(resp.Cookies(), "cc_session"); cookie != nil {
		t.Fatalf("desktop callback leaked a browser session cookie: %#v", cookie)
	}
	if cookie := findCookie(resp.Cookies(), "cc_github_desktop_challenge"); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("expected desktop challenge cookie to be cleared, got %#v", cookie)
	}
	resp.Body.Close()

	consume := func(origin, candidateVerifier string) *http.Response {
		t.Helper()
		body, err := json.Marshal(map[string]string{"code": grantCode, "code_verifier": candidateVerifier})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/github/desktop/consume", bytes.NewReader(body))
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
	resp = consume(server.URL, strings.Repeat("x", 43))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected wrong verifier rejection, got %s", resp.Status)
	}
	resp.Body.Close()
	resp = consume(server.URL, verifier)
	sessionCookie := findCookie(resp.Cookies(), "cc_session")
	if resp.StatusCode != http.StatusOK || sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected desktop session cookie, got %s %#v", resp.Status, sessionCookie)
	}
	resp.Body.Close()
	resp = consume(server.URL, verifier)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected one-time grant rejection, got %s", resp.Status)
	}
	resp.Body.Close()
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

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
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
	var stateCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "cc_github_state" {
			stateCookie = cookie
		}
	}
	resp.Body.Close()
	if stateCookie == nil || stateCookie.Value == "" {
		t.Fatal("expected github state cookie")
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+stateCookie.Value, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(stateCookie)
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
	clearedStateCookie := findCookie(resp.Cookies(), "cc_github_state")
	if clearedStateCookie == nil || clearedStateCookie.MaxAge >= 0 {
		t.Fatalf("expected github state cookie to be cleared, got %#v", clearedStateCookie)
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
			path := server.URL + "/api/auth/github/callback?state=" + stateCookie.Value
			if tc.code != "" {
				path += "&code=" + tc.code
			}
			req, err := http.NewRequest(http.MethodGet, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(stateCookie)
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
	if got := srv.githubRedirectURL(req); got != "https://public.example/api/auth/github/callback" {
		t.Fatalf("unexpected public redirect url %q", got)
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
	stateCookie := findCookie(resp.Cookies(), "cc_github_state")
	resp.Body.Close()
	if stateCookie == nil {
		t.Fatal("expected state cookie")
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=ok&state="+stateCookie.Value, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(stateCookie)
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
	stateCookie := findCookie(resp.Cookies(), "cc_github_state")
	resp.Body.Close()
	if stateCookie == nil {
		t.Fatal("expected state cookie")
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=member&state="+stateCookie.Value, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(stateCookie)
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

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/auth/github/callback?code=denied&state="+stateCookie.Value, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(stateCookie)
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
	req := httptest.NewRequest(http.MethodGet, "https://example.test/api/auth/github/callback?code=secret-code&state=secret-state&after_cursor=secret-cursor", nil)

	entry := formatter.NewLogEntry(req)
	entry.Write(http.StatusFound, 0, nil, time.Millisecond, nil)

	got := logs.String()
	if strings.Contains(got, "secret-code") || strings.Contains(got, "secret-state") || strings.Contains(got, "secret-cursor") || strings.Contains(got, "?") {
		t.Fatalf("expected query string to be omitted from request log, got %q", got)
	}
	if !strings.Contains(got, "/api/auth/github/callback") {
		t.Fatalf("expected path in request log, got %q", got)
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

	srv := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{ClientID: "c", ClientSecret: "s", TokenURL: "://bad", UserURL: "://bad"}})
	req := httptest.NewRequest(http.MethodGet, "https://example.test/callback", nil)
	req.TLS = &tls.ConnectionState{}
	if got := srv.githubRedirectURL(req); got != "https://example.test/api/auth/github/callback" {
		t.Fatalf("unexpected tls redirect %q", got)
	}
	if _, err := srv.exchangeGitHubCode(context.Background(), req, "x"); err == nil {
		t.Fatal("expected bad token url error")
	}
	if err := srv.githubGetJSON(context.Background(), "://bad", "token", &struct{}{}); err == nil {
		t.Fatal("expected bad github api url error")
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
