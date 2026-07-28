package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestGitHubProjectSetupCreatesWebhookAndProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "github-connect-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Member",
		Email:       "github-connect-member@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}

	var hookRequest struct {
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	repinged := make(chan struct{}, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if err := r.ParseForm(); err != nil || r.FormValue("code") != "setup-code" || r.FormValue("code_verifier") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "one-time-token"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/block/buzz/hooks":
			if r.Header.Get("Authorization") != "Bearer one-time-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&hookRequest); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int64{"id": 501})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/block/buzz/hooks/501/pings":
			repinged <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
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
		APIURL:       provider.URL,
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	expectStatusAsUser(
		t,
		member.ID,
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspace.ID+"/projects/github/connect",
		strings.NewReader(`{"name":"Denied","repositories":["block/buzz"]}`),
		http.StatusForbidden,
	)

	authorizationURL, bindingCookie := startProjectGitHubOAuth(t, client, server.URL, workspace.ID, owner.ID, map[string]any{
		"name":         "Buzz",
		"repositories": []string{"https://github.com/Block/Buzz.git"},
	})
	if authorizationURL.Query().Get("scope") != githubProjectWebhookScope {
		t.Fatalf("unexpected project OAuth scope %q", authorizationURL.Query().Get("scope"))
	}
	if authorizationURL.Query().Get("code_challenge_method") != "S256" || authorizationURL.Query().Get("code_challenge") == "" {
		t.Fatalf("expected PKCE authorization URL, got %s", authorizationURL)
	}

	callback := server.URL + "/api/auth/github/callback?code=setup-code&state=" + url.QueryEscape(authorizationURL.Query().Get("state"))
	req, err := http.NewRequest(http.MethodGet, callback, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
	req.AddCookie(bindingCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "/app/"+workspace.ID+"/") {
		t.Fatalf("unexpected setup callback: %s %s", resp.Status, resp.Header.Get("Location"))
	}

	if hookRequest.Config["content_type"] != "json" || hookRequest.Config["secret"] == "" ||
		!strings.HasPrefix(hookRequest.Config["url"], server.URL+"/api/hooks/github/projects/prj_") {
		t.Fatalf("unexpected GitHub hook request: %#v", hookRequest)
	}
	if strings.Join(hookRequest.Events, ",") != strings.Join(githubProjectWebhookEvents, ",") {
		t.Fatalf("unexpected GitHub hook events: %#v", hookRequest.Events)
	}
	select {
	case <-repinged:
	default:
		t.Fatal("automatic setup did not re-ping the committed project webhook")
	}
	projects := getJSONAsUser[struct {
		Projects []store.Project `json:"projects"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/projects")
	if len(projects.Projects) != 1 || projects.Projects[0].Repositories[0].FullName != "block/buzz" {
		t.Fatalf("automatic setup did not create the project: %#v", projects.Projects)
	}
	sendProjectWebhook(
		t,
		hookRequest.Config["url"],
		hookRequest.Config["secret"],
		"ping",
		"automatic-setup-ping",
		map[string]any{"repository": map[string]any{"full_name": "block/buzz"}},
		http.StatusOK,
	)
}

func TestGitHubProjectSetupRollsBackPartialHooks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "github-connect-rollback@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]

	deleted := make(chan string, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "rollback-token"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/block/buzz/hooks":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int64{"id": 601})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/private/denied/hooks":
			w.WriteHeader(http.StatusForbidden)
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/block/buzz/hooks/601":
			deleted <- r.URL.Path
			w.WriteHeader(http.StatusNoContent)
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
		APIURL:       provider.URL,
	}}).Handler())
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	authorizationURL, bindingCookie := startProjectGitHubOAuth(t, client, server.URL, workspace.ID, owner.ID, map[string]any{
		"name":         "Rollback",
		"repositories": []string{"block/buzz", "private/denied"},
	})
	callback := server.URL + "/api/auth/github/callback?code=setup-code&state=" + url.QueryEscape(authorizationURL.Query().Get("state"))
	req, err := http.NewRequest(http.MethodGet, callback, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
	req.AddCookie(bindingCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "reason=permission") {
		t.Fatalf("unexpected failed setup redirect: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	select {
	case path := <-deleted:
		if path != "/repos/block/buzz/hooks/601" {
			t.Fatalf("unexpected rollback path %q", path)
		}
	default:
		t.Fatal("successful repository hook was not rolled back")
	}
	projects := getJSONAsUser[struct {
		Projects []store.Project `json:"projects"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/projects")
	if len(projects.Projects) != 0 {
		t.Fatalf("failed automatic setup left a local project: %#v", projects.Projects)
	}
}

func startProjectGitHubOAuth(
	t *testing.T,
	client *http.Client,
	serverURL string,
	workspaceID string,
	userID string,
	body map[string]any,
) (*url.URL, *http.Cookie) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(
		http.MethodPost,
		serverURL+"/api/workspaces/"+workspaceID+"/projects/github/connect",
		bytes.NewReader(encoded),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected project OAuth start status: %s", resp.Status)
	}
	var result struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	bindingCookie := findCookie(resp.Cookies(), "cc_oauth_binding")
	if bindingCookie == nil {
		t.Fatalf("project OAuth start did not set a browser binding: %#v", resp.Cookies())
	}
	return authorizationURL, bindingCookie
}
