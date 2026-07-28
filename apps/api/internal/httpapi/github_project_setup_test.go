package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestGitHubProjectSetupSelectsRepositoriesAndCreatesProject(t *testing.T) {
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

	var repositoryRequests atomic.Int32
	var hookRequest struct {
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	repinged := make(chan struct{}, 1)
	revoked := make(chan string, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if err := r.ParseForm(); err != nil || r.FormValue("code") != "setup-code" || r.FormValue("code_verifier") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "temporary-repo-token"})
		case r.Method == http.MethodGet && r.URL.Path == "/user/repos":
			repositoryRequests.Add(1)
			if r.Header.Get("Authorization") != "Bearer temporary-repo-token" ||
				r.URL.Query().Get("affiliation") != "owner,collaborator,organization_member" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"name":        "Buzz",
					"full_name":   "Block/Buzz",
					"private":     true,
					"html_url":    "https://github.com/Block/Buzz",
					"description": "Admin repository",
					"owner":       map[string]string{"login": "Block"},
					"permissions": map[string]bool{"admin": true},
				},
				{
					"name":        "ReadOnly",
					"full_name":   "Block/ReadOnly",
					"html_url":    "https://github.com/Block/ReadOnly",
					"owner":       map[string]string{"login": "Block"},
					"permissions": map[string]bool{"admin": false},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/block/buzz/hooks":
			if r.Header.Get("Authorization") != "Bearer temporary-repo-token" {
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
		case r.Method == http.MethodDelete && r.URL.Path == "/applications/client/token":
			username, password, ok := r.BasicAuth()
			var body map[string]string
			if !ok || username != "client" || password != "secret" ||
				json.NewDecoder(r.Body).Decode(&body) != nil ||
				body["access_token"] != "temporary-repo-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			revoked <- body["access_token"]
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
		strings.NewReader(`{"name":"Denied"}`),
		http.StatusForbidden,
	)

	authorizationURL, bindingCookie := startProjectGitHubOAuth(t, client, server.URL, workspace.ID, owner.ID, map[string]any{
		"name":       "Buzz",
		"member_ids": []string{member.ID},
	})
	if authorizationURL.Query().Get("scope") != githubProjectRepositoryScope {
		t.Fatalf("unexpected project OAuth scope %q", authorizationURL.Query().Get("scope"))
	}
	if authorizationURL.Query().Get("code_challenge_method") != "S256" || authorizationURL.Query().Get("code_challenge") == "" {
		t.Fatalf("expected PKCE authorization URL, got %s", authorizationURL)
	}

	setupCookie, location := finishProjectGitHubOAuth(
		t,
		client,
		server.URL,
		owner.ID,
		authorizationURL,
		bindingCookie,
	)
	if !strings.Contains(location, "/app/"+workspace.ID+"/projects?github_setup=select") {
		t.Fatalf("unexpected setup callback location %q", location)
	}
	if !setupCookie.HttpOnly || setupCookie.MaxAge <= 0 || setupCookie.MaxAge > int(githubProjectSetupGrantTTL.Seconds()) {
		t.Fatalf("unexpected setup cookie attributes: %#v", setupCookie)
	}
	if !setupCookie.Secure {
		t.Fatalf("forwarded HTTPS callback did not set a secure setup cookie: %#v", setupCookie)
	}
	if strings.Contains(setupCookie.Value, "temporary-repo-token") {
		t.Fatal("setup cookie exposed the GitHub access token")
	}

	listRequest, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/workspaces/"+workspace.ID+"/projects/github/repositories",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	addProjectSetupCookies(listRequest, owner.ID, bindingCookie, setupCookie)
	listResponse, err := client.Do(listRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected repository list status: %s", listResponse.Status)
	}
	var listed struct {
		Repositories []githubProjectRepositoryOption `json:"repositories"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Repositories) != 1 || listed.Repositories[0].FullName != "block/buzz" || !listed.Repositories[0].Private {
		t.Fatalf("repository picker did not filter to admin repositories: %#v", listed.Repositories)
	}

	deniedRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspace.ID+"/projects/github/complete",
		strings.NewReader(`{"repositories":["Block/ReadOnly"]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	deniedRequest.Header.Set("Content-Type", "application/json")
	addProjectSetupCookies(deniedRequest, owner.ID, bindingCookie, setupCookie)
	deniedResponse, err := client.Do(deniedRequest)
	if err != nil {
		t.Fatal(err)
	}
	deniedResponse.Body.Close()
	if deniedResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin repository selection returned %s", deniedResponse.Status)
	}

	completeRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspace.ID+"/projects/github/complete",
		strings.NewReader(`{"repositories":["Block/Buzz"]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	completeRequest.Header.Set("Content-Type", "application/json")
	addProjectSetupCookies(completeRequest, owner.ID, bindingCookie, setupCookie)
	completeResponse, err := client.Do(completeRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer completeResponse.Body.Close()
	if completeResponse.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected setup completion status: %s", completeResponse.Status)
	}
	cleared := findCookie(completeResponse.Cookies(), "cc_github_project_setup")
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("successful setup did not clear its access grant: %#v", completeResponse.Cookies())
	}
	if repositoryRequests.Load() != 3 {
		t.Fatalf("expected repository access to be refreshed before completion, got %d requests", repositoryRequests.Load())
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
	select {
	case token := <-revoked:
		if token != "temporary-repo-token" {
			t.Fatalf("unexpected revoked token %q", token)
		}
	default:
		t.Fatal("automatic setup did not revoke its temporary GitHub token")
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

func TestGitHubProjectSetupRejectsTamperedGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "github-connect-tamper@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var repositoryRequests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "secret-token"})
		case "/user/repos":
			repositoryRequests.Add(1)
			_ = json.NewEncoder(w).Encode([]any{})
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

	authorizationURL, bindingCookie := startProjectGitHubOAuth(
		t,
		client,
		server.URL,
		workspaces[0].ID,
		owner.ID,
		map[string]any{"name": "Tamper"},
	)
	setupCookie, _ := finishProjectGitHubOAuth(t, client, server.URL, owner.ID, authorizationURL, bindingCookie)
	tamperIndex := len(setupCookie.Value) / 2
	last := setupCookie.Value[tamperIndex]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	setupCookie.Value = setupCookie.Value[:tamperIndex] + string(replacement) + setupCookie.Value[tamperIndex+1:]

	req, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/workspaces/"+workspaces[0].ID+"/projects/github/repositories",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	addProjectSetupCookies(req, owner.ID, bindingCookie, setupCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered setup grant returned %s", resp.Status)
	}
	if repositoryRequests.Load() != 0 {
		t.Fatal("tampered setup grant reached GitHub")
	}
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
		case r.Method == http.MethodGet && r.URL.Path == "/user/repos":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"name":        "buzz",
					"full_name":   "block/buzz",
					"html_url":    "https://github.com/block/buzz",
					"owner":       map[string]string{"login": "block"},
					"permissions": map[string]bool{"admin": true},
				},
				{
					"name":        "denied",
					"full_name":   "private/denied",
					"html_url":    "https://github.com/private/denied",
					"owner":       map[string]string{"login": "private"},
					"permissions": map[string]bool{"admin": true},
				},
			})
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

	authorizationURL, bindingCookie := startProjectGitHubOAuth(
		t,
		client,
		server.URL,
		workspace.ID,
		owner.ID,
		map[string]any{"name": "Rollback"},
	)
	setupCookie, _ := finishProjectGitHubOAuth(t, client, server.URL, owner.ID, authorizationURL, bindingCookie)
	req, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspace.ID+"/projects/github/complete",
		strings.NewReader(`{"repositories":["block/buzz","private/denied"]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	addProjectSetupCookies(req, owner.ID, bindingCookie, setupCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected failed setup status: %s", resp.Status)
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

func finishProjectGitHubOAuth(
	t *testing.T,
	client *http.Client,
	serverURL string,
	userID string,
	authorizationURL *url.URL,
	bindingCookie *http.Cookie,
) (*http.Cookie, string) {
	t.Helper()
	callback := serverURL + "/api/auth/github/callback?code=setup-code&state=" +
		url.QueryEscape(authorizationURL.Query().Get("state"))
	req, err := http.NewRequest(http.MethodGet, callback, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(bindingCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unexpected setup callback: %s %s", resp.Status, resp.Header.Get("Location"))
	}
	setupCookie := findCookie(resp.Cookies(), "cc_github_project_setup")
	if setupCookie == nil {
		t.Fatalf("project OAuth callback did not set a setup grant: %#v", resp.Cookies())
	}
	return setupCookie, resp.Header.Get("Location")
}

func addProjectSetupCookies(
	req *http.Request,
	userID string,
	bindingCookie *http.Cookie,
	setupCookie *http.Cookie,
) {
	req.Header.Set("X-ClickClack-User", userID)
	req.AddCookie(bindingCookie)
	req.AddCookie(setupCookie)
}
