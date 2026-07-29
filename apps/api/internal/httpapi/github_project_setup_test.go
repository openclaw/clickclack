package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	pendingRevocations, err := st.ListPendingGitHubTokenRevocations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingRevocations) != 1 ||
		strings.Contains(pendingRevocations[0].EncryptedToken, "temporary-repo-token") {
		t.Fatalf("temporary token was not durably encrypted for revocation: %#v", pendingRevocations)
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

	emptyRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspace.ID+"/projects/github/complete",
		strings.NewReader(`{"repositories":[]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	emptyRequest.Header.Set("Content-Type", "application/json")
	addProjectSetupCookies(emptyRequest, owner.ID, bindingCookie, setupCookie)
	emptyResponse, err := client.Do(emptyRequest)
	if err != nil {
		t.Fatal(err)
	}
	emptyResponse.Body.Close()
	if emptyResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty repository selection returned %s", emptyResponse.Status)
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
	pendingRevocations, err = st.ListPendingGitHubTokenRevocations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingRevocations) != 0 {
		t.Fatalf("successful revocation left durable cleanup state: %#v", pendingRevocations)
	}
	projects := getJSONAsUser[struct {
		Projects []store.Project `json:"projects"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/projects")
	if len(projects.Projects) != 1 || projects.Projects[0].Name != "Buzz" ||
		projects.Projects[0].Slug != "block-buzz" ||
		projects.Projects[0].Repositories[0].FullName != "block/buzz" {
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

func TestGitHubProjectSetupRevokesTokenWhenCallbackSessionChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "github-connect-session-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Other",
		Email:       "github-connect-session-other@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	revoked := make(chan string, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "session-change-token"})
		case r.Method == http.MethodDelete && r.URL.Path == "/applications/client/token":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
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
	authorizationURL, bindingCookie := startProjectGitHubOAuth(
		t,
		client,
		server.URL,
		workspaces[0].ID,
		owner.ID,
		map[string]any{},
	)
	callback := server.URL + "/api/auth/github/callback?code=setup-code&state=" +
		url.QueryEscape(authorizationURL.Query().Get("state"))
	req, err := http.NewRequest(http.MethodGet, callback, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", other.ID)
	req.AddCookie(bindingCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unexpected callback response: %s", resp.Status)
	}
	if findCookie(resp.Cookies(), "cc_github_project_setup") != nil {
		t.Fatal("session-mismatched callback received a setup grant")
	}
	select {
	case token := <-revoked:
		if token != "session-change-token" {
			t.Fatalf("unexpected revoked token %q", token)
		}
	default:
		t.Fatal("session-mismatched callback did not revoke its token")
	}
	pending, err := st.ListPendingGitHubTokenRevocations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("session-mismatched callback left pending state: %#v", pending)
	}
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
		map[string]any{},
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
	var deleteRequests atomic.Int32
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
			if deleteRequests.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
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
		map[string]any{},
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
	if deleteRequests.Load() != 2 {
		t.Fatalf("expected rollback retry after GitHub failure, got %d delete requests", deleteRequests.Load())
	}
	projects := getJSONAsUser[struct {
		Projects []store.Project `json:"projects"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/projects")
	if len(projects.Projects) != 0 {
		t.Fatalf("failed automatic setup left a local project: %#v", projects.Projects)
	}
}

func TestGitHubProjectSetupRestoresPendingTokenRevocation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	revoked := make(chan string, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/applications/client/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		revoked <- body["access_token"]
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(provider.Close)
	options := Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		APIURL:       provider.URL,
	}}
	server := &Server{
		store:                st,
		githubOAuth:          options.GitHubOAuth.withDefaults(),
		githubRevocationJobs: make(map[string]githubProjectRevocationJob),
	}
	now := time.Now().UTC()
	for index, token := range []string{"restart-token-1", "restart-token-2"} {
		encryptedToken, err := server.sealGitHubProjectSetupToken(token)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.CreatePendingGitHubTokenRevocation(ctx, store.PendingGitHubTokenRevocation{
			ID:             fmt.Sprintf("gtr_restart_%d", index),
			EncryptedToken: encryptedToken,
			CreatedAt:      now,
			RevokeAfter:    now.Add(50 * time.Millisecond),
		}); err != nil {
			t.Fatal(err)
		}
	}

	server.restorePendingGitHubTokenRevocationsWithLimit(1)
	revokedTokens := make(map[string]struct{}, 2)
	deadline := time.Now().Add(2 * time.Second)
	for len(revokedTokens) < 2 {
		select {
		case token := <-revoked:
			revokedTokens[token] = struct{}{}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("pending token revocation batch did not continue: %#v", revokedTokens)
		}
	}
	for _, token := range []string{"restart-token-1", "restart-token-2"} {
		if _, ok := revokedTokens[token]; !ok {
			t.Fatalf("pending token %q was not restored", token)
		}
	}
	for {
		pending, err := st.ListPendingGitHubTokenRevocations(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restored revocation was not removed: %#v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGitHubProjectSetupStartValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "github-connect-validation@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	unconfigured := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(unconfigured.Close)
	configured := httptest.NewServer(New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      "https://github.test/authorize",
		TokenURL:     "https://github.test/token",
		APIURL:       "https://api.github.test",
	}}).Handler())
	t.Cleanup(configured.Close)

	post := func(serverURL, workspaceID, userID, body string) int {
		t.Helper()
		request, err := http.NewRequest(
			http.MethodPost,
			serverURL+"/api/workspaces/"+workspaceID+"/projects/github/connect",
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if userID != "" {
			request.Header.Set("X-ClickClack-User", userID)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}

	if status := post(unconfigured.URL, workspaces[0].ID, owner.ID, `{}`); status != http.StatusNotImplemented {
		t.Fatalf("unconfigured GitHub OAuth returned %d", status)
	}
	if status := post(configured.URL, workspaces[0].ID, "missing-user", `{}`); status != http.StatusUnauthorized {
		t.Fatalf("unknown-user setup returned %d", status)
	}
	if status := post(configured.URL, workspaces[0].ID, owner.ID, `{`); status != http.StatusBadRequest {
		t.Fatalf("malformed setup returned %d", status)
	}
	if status := post(configured.URL, "ws_01ARZ3NDEKTSV4RRFFQ69G5FAV", owner.ID, `{}`); status != http.StatusBadRequest {
		t.Fatalf("unknown workspace setup returned %d", status)
	}
	invalidBodies := []string{
		`{"description":"` + strings.Repeat("x", 501) + `"}`,
		func() string {
			memberIDs := make([]string, githubProjectSetupMaxMembers+1)
			for index := range memberIDs {
				memberIDs[index] = fmt.Sprintf("usr_%d", index)
			}
			encoded, err := json.Marshal(map[string]any{"member_ids": memberIDs})
			if err != nil {
				t.Fatal(err)
			}
			return string(encoded)
		}(),
		`{"member_ids":[""]}`,
	}
	for index, body := range invalidBodies {
		if status := post(configured.URL, workspaces[0].ID, owner.ID, body); status != http.StatusBadRequest {
			t.Fatalf("invalid setup %d returned %d", index, status)
		}
	}
}

func TestGitHubProjectSetupDraftAndGrantValidation(t *testing.T) {
	t.Parallel()
	validDraft := githubProjectOAuthDraft{
		ProjectID:     "prj_test",
		WorkspaceID:   "ws_test",
		UserID:        "usr_test",
		WebhookSecret: "webhook-secret",
		MemberIDs:     []string{"usr_member"},
	}
	draftCases := []githubProjectOAuthDraft{
		func() githubProjectOAuthDraft {
			draft := validDraft
			draft.Description = strings.Repeat("x", 501)
			return draft
		}(),
		func() githubProjectOAuthDraft {
			draft := validDraft
			draft.ProjectID = ""
			return draft
		}(),
		func() githubProjectOAuthDraft {
			draft := validDraft
			draft.MemberIDs = make([]string, githubProjectSetupMaxMembers+1)
			return draft
		}(),
		func() githubProjectOAuthDraft {
			draft := validDraft
			draft.MemberIDs = []string{""}
			return draft
		}(),
		func() githubProjectOAuthDraft {
			draft := validDraft
			draft.MemberIDs = []string{strings.Repeat("x", githubProjectSetupMaxMemberID+1)}
			return draft
		}(),
	}
	for index, draft := range draftCases {
		if err := validateGitHubProjectDraft(draft); err == nil {
			t.Fatalf("invalid draft %d was accepted", index)
		}
	}
	if err := validateGitHubProjectDraft(validDraft); err != nil {
		t.Fatalf("valid draft rejected: %v", err)
	}

	st := newEmptyHTTPStore(t)
	s := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
	}})
	now := time.Now().UTC()
	binding := "browser-binding"
	validGrant := githubProjectSetupGrant{
		Version:            githubProjectSetupGrantVersion,
		AccessToken:        "temporary-token",
		RevocationID:       "gtr_test",
		BrowserBindingHash: secretHash(binding),
		Draft:              validDraft,
		IssuedAt:           now.Add(-time.Minute).Unix(),
		ExpiresAt:          now.Add(time.Minute).Unix(),
	}
	grantRequest := func(value, browserBinding string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, "https://clickclack.test/projects", nil)
		if value != "" {
			request.AddCookie(&http.Cookie{Name: s.cookies.GitHubProjectSetup, Value: value})
		}
		if browserBinding != "" {
			request.AddCookie(&http.Cookie{Name: s.cookies.OAuthBinding, Value: browserBinding})
		}
		return request
	}
	sealGrant := func(grant githubProjectSetupGrant) string {
		t.Helper()
		value, err := s.sealGitHubProjectSetupGrant(grant)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	if _, err := s.githubProjectSetupGrant(grantRequest("", "")); err == nil {
		t.Fatal("missing setup grant cookie was accepted")
	}
	if _, err := s.githubProjectSetupGrant(grantRequest("%", binding)); err == nil {
		t.Fatal("invalid setup grant encoding was accepted")
	}
	shortValue := base64.RawURLEncoding.EncodeToString([]byte("short"))
	if _, err := s.githubProjectSetupGrant(grantRequest(shortValue, binding)); err == nil {
		t.Fatal("short setup grant was accepted")
	}

	gcm, err := s.githubProjectSetupAEAD()
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	invalidJSON := base64.RawURLEncoding.EncodeToString(
		gcm.Seal(nonce, nonce, []byte("{"), s.githubProjectSetupCookieAAD()),
	)
	if _, err := s.githubProjectSetupGrant(grantRequest(invalidJSON, binding)); err == nil {
		t.Fatal("non-JSON setup grant was accepted")
	}

	tampered, err := base64.RawURLEncoding.DecodeString(sealGrant(validGrant))
	if err != nil {
		t.Fatal(err)
	}
	tampered[len(tampered)-1] ^= 0xff
	if _, err := s.githubProjectSetupGrant(grantRequest(
		base64.RawURLEncoding.EncodeToString(tampered),
		binding,
	)); err == nil {
		t.Fatal("tampered setup grant was accepted")
	}

	invalidGrants := []githubProjectSetupGrant{
		func() githubProjectSetupGrant {
			grant := validGrant
			grant.ExpiresAt = now.Add(-time.Minute).Unix()
			return grant
		}(),
		func() githubProjectSetupGrant {
			grant := validGrant
			grant.IssuedAt = now.Add(time.Minute).Unix()
			return grant
		}(),
		func() githubProjectSetupGrant {
			grant := validGrant
			grant.RevocationID = ""
			return grant
		}(),
	}
	for index, grant := range invalidGrants {
		if _, err := s.githubProjectSetupGrant(grantRequest(sealGrant(grant), binding)); err == nil {
			t.Fatalf("invalid grant %d was accepted", index)
		}
	}
	if _, err := s.githubProjectSetupGrant(grantRequest(sealGrant(validGrant), "wrong-binding")); err == nil {
		t.Fatal("setup grant with the wrong browser binding was accepted")
	}
	invalidDraftGrant := validGrant
	invalidDraftGrant.Draft.WebhookSecret = ""
	if _, err := s.githubProjectSetupGrant(grantRequest(sealGrant(invalidDraftGrant), binding)); err == nil {
		t.Fatal("setup grant with an invalid draft was accepted")
	}
	grant, err := s.githubProjectSetupGrant(grantRequest(sealGrant(validGrant), binding))
	if err != nil || grant.AccessToken != validGrant.AccessToken {
		t.Fatalf("valid setup grant rejected: %#v, %v", grant, err)
	}

	oversizedGrant := validGrant
	oversizedGrant.AccessToken = strings.Repeat("x", githubProjectSetupCookieMax)
	if err := s.setGitHubProjectSetupGrant(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "https://clickclack.test/projects", nil),
		oversizedGrant,
	); err == nil {
		t.Fatal("oversized setup grant was accepted")
	}

	encryptedToken, err := s.sealGitHubProjectSetupToken("temporary-token")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.unsealGitHubProjectSetupToken(encryptedToken)
	if err != nil || token != "temporary-token" {
		t.Fatalf("encrypted token did not round trip: %q, %v", token, err)
	}
	for _, value := range []string{"%", shortValue} {
		if _, err := s.unsealGitHubProjectSetupToken(value); err == nil {
			t.Fatalf("invalid encrypted token %q was accepted", value)
		}
	}
	encryptedBytes, err := base64.RawURLEncoding.DecodeString(encryptedToken)
	if err != nil {
		t.Fatal(err)
	}
	encryptedBytes[len(encryptedBytes)-1] ^= 0xff
	if _, err := s.unsealGitHubProjectSetupToken(
		base64.RawURLEncoding.EncodeToString(encryptedBytes),
	); err == nil {
		t.Fatal("tampered encrypted token was accepted")
	}
	emptyToken, err := s.sealGitHubProjectSetupToken("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.unsealGitHubProjectSetupToken(emptyToken); err == nil {
		t.Fatal("empty encrypted token was accepted")
	}

	unconfigured := &Server{}
	unconfigured.clearGitHubProjectSetupTokenRevocation("")
	unconfigured.queuePendingGitHubTokenRevocationRestore()
	if unconfigured.githubRestoreQueued {
		t.Fatal("unconfigured GitHub OAuth queued a token restoration")
	}
	unconfigured.scheduleGitHubProjectSetupTokenRevocation("gtr_direct", "token", time.Hour)
	if _, ok := unconfigured.githubRevocationJobs["gtr_direct"]; !ok {
		t.Fatal("directly constructed server did not initialize its revocation jobs")
	}
	unconfigured.clearGitHubProjectSetupTokenRevocation("gtr_direct")
}

func TestGitHubProjectSetupCancellationRevokesToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "github-connect-cancel@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	revoked := make(chan string, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "cancel-token"})
		case r.Method == http.MethodDelete && r.URL.Path == "/applications/client/token":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
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
	authorizationURL, bindingCookie := startProjectGitHubOAuth(
		t,
		client,
		server.URL,
		workspaces[0].ID,
		owner.ID,
		map[string]any{},
	)
	setupCookie, _ := finishProjectGitHubOAuth(
		t,
		client,
		server.URL,
		owner.ID,
		authorizationURL,
		bindingCookie,
	)

	cancelRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspaces[0].ID+"/projects/github/cancel",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	addProjectSetupCookies(cancelRequest, owner.ID, bindingCookie, setupCookie)
	cancelResponse, err := client.Do(cancelRequest)
	if err != nil {
		t.Fatal(err)
	}
	cancelResponse.Body.Close()
	if cancelResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected cancel status: %s", cancelResponse.Status)
	}
	cleared := findCookie(cancelResponse.Cookies(), "cc_github_project_setup")
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("cancel did not clear its setup grant: %#v", cancelResponse.Cookies())
	}
	select {
	case token := <-revoked:
		if token != "cancel-token" {
			t.Fatalf("unexpected revoked token %q", token)
		}
	default:
		t.Fatal("cancel did not revoke its temporary token")
	}

	expiredRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/workspaces/"+workspaces[0].ID+"/projects/github/cancel",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	expiredRequest.Header.Set("X-ClickClack-User", owner.ID)
	expiredResponse, err := client.Do(expiredRequest)
	if err != nil {
		t.Fatal(err)
	}
	expiredResponse.Body.Close()
	if expiredResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing setup grant returned %s", expiredResponse.Status)
	}
}

func TestGitHubProjectSetupGitHubAPIErrorPaths(t *testing.T) {
	t.Parallel()
	var repositoryMode atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user/repos":
			switch repositoryMode.Load() {
			case 1:
				w.WriteHeader(http.StatusInternalServerError)
			case 2:
				_, _ = w.Write([]byte("{"))
			default:
				page := r.URL.Query().Get("page")
				repositories := make([]map[string]any, githubRepositoryPageSize)
				for index := range repositories {
					name := "repo-" + page + "-" + fmt.Sprint(index)
					repositories[index] = map[string]any{
						"name":        name,
						"full_name":   "acme/" + name,
						"html_url":    "https://github.com/acme/" + name,
						"owner":       map[string]string{"login": "acme"},
						"permissions": map[string]bool{"admin": true},
					}
				}
				_ = json.NewEncoder(w).Encode(repositories)
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/applications/client/token":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/denied/hooks":
			w.WriteHeader(http.StatusForbidden)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/malformed/hooks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{"))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/missing/hooks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":0}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/acme/delete/hooks/2":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/ping/hooks/1/pings":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)
	st := newEmptyHTTPStore(t)
	s := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		APIURL:       provider.URL,
	}})
	request := httptest.NewRequest(http.MethodGet, "https://clickclack.test/projects", nil)

	repositoryMode.Store(1)
	if _, _, err := s.fetchGitHubProjectRepositories(request, "token"); err == nil {
		t.Fatal("GitHub repository status failure was accepted")
	}
	repositoryMode.Store(2)
	if _, _, err := s.fetchGitHubProjectRepositories(request, "token"); err == nil {
		t.Fatal("malformed GitHub repository response was accepted")
	}
	repositoryMode.Store(3)
	repositories, truncated, err := s.fetchGitHubProjectRepositories(request, "token")
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(repositories) != githubRepositoryPageSize*githubRepositoryMaxPages {
		t.Fatalf("unexpected paginated repositories: count=%d truncated=%v", len(repositories), truncated)
	}

	for _, name := range []string{"denied", "malformed", "missing"} {
		repository := store.CreateProjectRepositoryInput{
			Owner:    "acme",
			Name:     name,
			FullName: "acme/" + name,
		}
		if _, err := s.createGitHubRepositoryWebhook(
			request,
			"token",
			repository,
			"https://clickclack.test/hook",
			"secret",
		); err == nil {
			t.Fatalf("invalid webhook response for %s was accepted", name)
		}
	}
	hook := githubCreatedHook{
		Repository: store.CreateProjectRepositoryInput{
			Owner:    "acme",
			Name:     "delete",
			FullName: "acme/delete",
		},
		ID: 2,
	}
	if err := s.deleteGitHubRepositoryWebhooks(request.Context(), "token", []githubCreatedHook{hook}); err == nil {
		t.Fatal("failed webhook rollback was reported as successful")
	}
	if err := s.deleteGitHubRepositoryWebhooks(request.Context(), "token", nil); err != nil {
		t.Fatalf("empty webhook rollback failed: %v", err)
	}
	s.rollbackGitHubRepositoryWebhooks(request, "token", nil)
	pingHook := githubCreatedHook{
		Repository: store.CreateProjectRepositoryInput{
			Owner:    "acme",
			Name:     "ping",
			FullName: "acme/ping",
		},
		ID: 1,
	}
	if err := s.pingGitHubRepositoryWebhook(request, "token", pingHook); err == nil {
		t.Fatal("failed webhook ping was reported as successful")
	}
	if err := s.revokeGitHubProjectSetupToken(request.Context(), "token"); err == nil {
		t.Fatal("failed token revocation was reported as successful")
	}
	s.revokeGitHubProjectSetupTokenOrRetry(request.Context(), "", "token", "test")
	s.rollbackGitHubRepositoryWebhooks(request, "token", []githubCreatedHook{hook})

	invalidURLServer := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		APIURL:       "://invalid",
	}})
	if _, _, err := invalidURLServer.fetchGitHubProjectRepositories(request, "token"); err == nil {
		t.Fatal("invalid GitHub repository API URL was accepted")
	}
	invalidRepository := store.CreateProjectRepositoryInput{
		Owner:    "acme",
		Name:     "repo",
		FullName: "acme/repo",
	}
	if _, err := invalidURLServer.createGitHubRepositoryWebhook(
		request,
		"token",
		invalidRepository,
		"https://clickclack.test/hook",
		"secret",
	); err == nil {
		t.Fatal("invalid GitHub webhook API URL was accepted")
	}
	invalidHook := githubCreatedHook{Repository: invalidRepository, ID: 1}
	if err := invalidURLServer.deleteGitHubRepositoryWebhook(
		request.Context(),
		"token",
		invalidHook,
	); err == nil {
		t.Fatal("invalid GitHub webhook delete URL was accepted")
	}
	if err := invalidURLServer.pingGitHubRepositoryWebhook(request, "token", invalidHook); err == nil {
		t.Fatal("invalid GitHub webhook ping URL was accepted")
	}
	if err := invalidURLServer.revokeGitHubProjectSetupToken(request.Context(), "token"); err == nil {
		t.Fatal("invalid GitHub token revocation URL was accepted")
	}

	transportFailureServer := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		APIURL:       "https://api.github.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("GitHub transport unavailable")
		})},
	}})
	if err := transportFailureServer.revokeGitHubProjectSetupToken(request.Context(), "token"); err == nil {
		t.Fatal("GitHub token revocation transport failure was accepted")
	}
	if _, err := transportFailureServer.createGitHubRepositoryWebhook(
		request,
		"token",
		invalidRepository,
		"https://clickclack.test/hook",
		"secret",
	); err == nil {
		t.Fatal("GitHub webhook creation transport failure was accepted")
	}
	if err := transportFailureServer.deleteGitHubRepositoryWebhook(
		request.Context(),
		"token",
		invalidHook,
	); err == nil {
		t.Fatal("GitHub webhook deletion transport failure was accepted")
	}
	if err := transportFailureServer.pingGitHubRepositoryWebhook(request, "token", invalidHook); err == nil {
		t.Fatal("GitHub webhook ping transport failure was accepted")
	}
	if got := (&githubHookError{Repository: "acme/repo", StatusCode: http.StatusForbidden}).Error(); got == "" {
		t.Fatal("GitHub hook error string was empty")
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
