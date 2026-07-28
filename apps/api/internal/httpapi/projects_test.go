package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestGitHubProjectRoomHTTPFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "project-http-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Reviewer", Email: "project-http-reviewer@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(t.TempDir(), "uploads")}).Handler())
	t.Cleanup(server.Close)

	expectStatusAsUser(
		t, "missing-user", http.MethodGet, server.URL+"/api/workspaces/"+workspace.ID+"/projects", nil, http.StatusUnauthorized,
	)
	expectStatusAsUser(
		t, "missing-user", http.MethodGet, server.URL+"/api/projects/missing-project", nil, http.StatusUnauthorized,
	)
	expectStatusAsUser(
		t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/projects",
		strings.NewReader(`{"name":"Denied","repositories":["block/buzz"]}`), http.StatusForbidden,
	)
	expectStatusAsUser(
		t, owner.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/projects",
		strings.NewReader(`{"name":`), http.StatusBadRequest,
	)
	for _, body := range []string{
		`{"name":"Invalid repository","repositories":["https://example.com/block/buzz"]}`,
		`{"name":"Empty repository","repositories":[""]}`,
		`{"name":"Incomplete repository","repositories":["block"]}`,
	} {
		expectStatusAsUser(
			t, owner.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/projects",
			strings.NewReader(body), http.StatusBadRequest,
		)
	}
	created := postJSONAsUser[struct {
		Project store.Project `json:"project"`
		Webhook struct {
			URL    string `json:"url"`
			Secret string `json:"secret"`
		} `json:"webhook"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/projects", map[string]any{
		"description":  "Shared PR room",
		"repositories": []string{"https://github.com/Block/Buzz.git", "block/buzz"},
		"member_ids":   []string{member.ID},
	})
	if len(created.Project.Repositories) != 1 || created.Project.Repositories[0].FullName != "block/buzz" {
		t.Fatalf("expected normalized, deduplicated repositories, got %#v", created.Project.Repositories)
	}
	if created.Project.Name != "buzz" {
		t.Fatalf("project name = %q, want primary repository name", created.Project.Name)
	}
	if created.Webhook.Secret == "" || created.Webhook.URL != server.URL+"/api/hooks/github/projects/"+created.Project.ID {
		t.Fatalf("unexpected webhook handoff: %#v", created.Webhook)
	}
	listResponse := getJSONAsUser[struct {
		Projects []store.Project `json:"projects"`
	}](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/projects")
	if len(listResponse.Projects) != 1 || listResponse.Projects[0].ID != created.Project.ID {
		t.Fatalf("unexpected project list: %#v", listResponse.Projects)
	}
	projectResponse := getJSONAsUser[struct {
		Project store.Project `json:"project"`
	}](t, member.ID, server.URL+"/api/projects/"+created.Project.ID)
	if projectResponse.Project.ID != created.Project.ID {
		t.Fatalf("unexpected project response: %#v", projectResponse.Project)
	}
	expectStatusAsUser(
		t, member.ID, http.MethodGet, server.URL+"/api/projects/missing-project", nil, http.StatusBadRequest,
	)
	contextResponse := getJSONAsUser[struct {
		Project store.Project `json:"project"`
		Context struct {
			SourceOfTruth string `json:"source_of_truth"`
			WriteAccess   bool   `json:"write_access"`
		} `json:"context"`
	}](t, member.ID, server.URL+"/api/projects/"+created.Project.ID+"/context")
	if contextResponse.Project.ID != created.Project.ID || contextResponse.Context.SourceOfTruth != "github" || contextResponse.Context.WriteAccess {
		t.Fatalf("unexpected project context: %#v", contextResponse)
	}

	opened := map[string]any{
		"action":     "opened",
		"repository": map[string]any{"full_name": "block/buzz"},
		"sender":     map[string]any{"login": "alice"},
		"pull_request": map[string]any{
			"number": 42, "title": "Coordinate project context", "html_url": "https://github.com/block/buzz/pull/42",
			"user": map[string]any{"login": "alice"},
			"head": map[string]any{"ref": "project-context"},
			"base": map[string]any{"ref": "main"},
		},
	}
	claim, err := st.ClaimGitHubDelivery(ctx, created.Project.ID, "delivery-active", "pull_request")
	if err != nil || claim != store.GitHubDeliveryClaimed {
		t.Fatalf("could not create active delivery fixture: claim=%q err=%v", claim, err)
	}
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "pull_request", "delivery-active", opened, http.StatusServiceUnavailable)
	if err := st.FailGitHubDelivery(ctx, created.Project.ID, "delivery-active"); err != nil {
		t.Fatal(err)
	}

	failingStore := &failGitHubThreadOnceStore{Store: st}
	retryServer := httptest.NewServer(New(failingStore, realtime.NewHub(), Options{
		UploadDir: filepath.Join(t.TempDir(), "retry-uploads"),
	}).Handler())
	t.Cleanup(retryServer.Close)
	retryEndpoint := retryServer.URL + "/api/hooks/github/projects/" + created.Project.ID
	retryOpened := map[string]any{
		"action":     "opened",
		"repository": map[string]any{"full_name": "block/buzz"},
		"sender":     map[string]any{"login": "retry-author"},
		"pull_request": map[string]any{
			"number": 45, "title": "Recover a failed delivery", "html_url": "https://github.com/block/buzz/pull/45",
			"user": map[string]any{"login": "retry-author"},
			"head": map[string]any{"ref": "retry-delivery"},
			"base": map[string]any{"ref": "main"},
		},
	}
	sendProjectWebhook(t, retryEndpoint, created.Webhook.Secret, "pull_request", "delivery-retry", retryOpened, http.StatusInternalServerError)
	sendProjectWebhook(t, retryEndpoint, created.Webhook.Secret, "pull_request", "delivery-retry", retryOpened, http.StatusAccepted)

	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "pull_request", "delivery-open", opened, http.StatusAccepted)
	messages, err := st.ListMessages(ctx, created.Project.Channel.ID, owner.ID, store.MessagePageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var pull42Root store.Message
	var retryRoots int
	for _, message := range messages.Messages {
		if strings.Contains(message.Body, "PR #42") {
			pull42Root = message
		}
		if strings.Contains(message.Body, "PR #45") {
			retryRoots++
		}
	}
	if len(messages.Messages) != 2 || pull42Root.ID == "" || retryRoots != 1 {
		t.Fatalf("expected one root per recovered and successful delivery, got %#v", messages.Messages)
	}
	messages.Messages = []store.Message{pull42Root}

	opened["action"] = "synchronize"
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "pull_request", "delivery-sync", opened, http.StatusAccepted)
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "pull_request", "delivery-sync", opened, http.StatusAccepted)
	_, replies, state, err := st.GetThread(ctx, messages.Messages[0].ID, owner.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || state.ReplyCount != 1 || !strings.Contains(replies[0].Body, "pushed new commits") {
		t.Fatalf("expected one idempotent synchronize reply, replies=%#v state=%#v", replies, state)
	}

	checkRun := map[string]any{
		"action":     "completed",
		"repository": map[string]any{"full_name": "block/buzz"},
		"check_run": map[string]any{
			"name": "tests", "conclusion": "failure",
			"pull_requests": []map[string]any{{"number": 43}},
		},
	}
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "check_run", "delivery-check-first", checkRun, http.StatusAccepted)
	channelMessages, err := st.ListMessages(ctx, created.Project.Channel.ID, owner.ID, store.MessagePageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var checkRoot store.Message
	for _, message := range channelMessages.Messages {
		if strings.Contains(message.Body, "PR #43") {
			checkRoot = message
			break
		}
	}
	if checkRoot.ID == "" {
		t.Fatalf("check event did not create a placeholder PR thread: %#v", channelMessages.Messages)
	}
	_, checkReplies, _, err := st.GetThread(ctx, checkRoot.ID, owner.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkReplies) != 1 || !strings.Contains(checkReplies[0].Body, "failure") {
		t.Fatalf("check result was not preserved in its PR thread: %#v", checkReplies)
	}
	openedAfterCheck := map[string]any{
		"action":     "opened",
		"repository": map[string]any{"full_name": "block/buzz"},
		"sender":     map[string]any{"login": "bob"},
		"pull_request": map[string]any{
			"number": 43, "title": "Fix the build", "html_url": "https://github.com/block/buzz/pull/43",
			"user": map[string]any{"login": "bob"},
			"head": map[string]any{"ref": "fix-build"},
			"base": map[string]any{"ref": "main"},
		},
	}
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "pull_request", "delivery-open-after-check", openedAfterCheck, http.StatusAccepted)
	enrichedRoot, err := st.GetMessage(ctx, checkRoot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enrichedRoot.Body, "Fix the build") || !strings.Contains(enrichedRoot.Body, "fix-build") {
		t.Fatalf("PR open event did not enrich the placeholder root: %#v", enrichedRoot)
	}

	issueOpened := map[string]any{
		"action":     "opened",
		"repository": map[string]any{"full_name": "block/buzz"},
		"sender":     map[string]any{"login": "carol"},
		"issue": map[string]any{
			"number": 44, "title": "Track the release blocker", "body": "Waiting on production access.",
			"html_url": "https://github.com/block/buzz/issues/44", "user": map[string]any{"login": "carol"},
		},
	}
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "issues", "delivery-issue-open", issueOpened, http.StatusAccepted)
	channelMessages, err = st.ListMessages(ctx, created.Project.Channel.ID, owner.ID, store.MessagePageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var issueRoot store.Message
	for _, message := range channelMessages.Messages {
		if strings.Contains(message.Body, "Issue #44") {
			issueRoot = message
			break
		}
	}
	if issueRoot.ID == "" || !strings.Contains(issueRoot.Body, "Waiting on production access") {
		t.Fatalf("issue event did not create a rich root: %#v", channelMessages.Messages)
	}

	issueComment := map[string]any{
		"action":     "created",
		"repository": map[string]any{"full_name": "block/buzz"},
		"sender":     map[string]any{"login": "dana"},
		"issue": map[string]any{
			"number": 44, "title": "Track the release blocker",
			"html_url": "https://github.com/block/buzz/issues/44", "user": map[string]any{"login": "carol"},
		},
		"comment": map[string]any{
			"body": "Access is ready.", "html_url": "https://github.com/block/buzz/issues/44#issuecomment-1",
			"user": map[string]any{"login": "dana"},
		},
	}
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "issue_comment", "delivery-issue-comment", issueComment, http.StatusAccepted)
	sendProjectWebhook(t, created.Webhook.URL, created.Webhook.Secret, "issue_comment", "delivery-issue-comment", issueComment, http.StatusAccepted)
	_, issueReplies, issueState, err := st.GetThread(ctx, issueRoot.ID, owner.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(issueReplies) != 1 || issueState.ReplyCount != 1 || !strings.Contains(issueReplies[0].Body, "Access is ready") {
		t.Fatalf("expected one idempotent issue reply, replies=%#v state=%#v", issueReplies, issueState)
	}

	sendProjectWebhook(t, created.Webhook.URL, "wrong-secret", "pull_request", "delivery-invalid", opened, http.StatusUnauthorized)
	_, replies, _, err = st.GetThread(ctx, messages.Messages[0].ID, owner.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 {
		t.Fatalf("invalid signature changed thread: %#v", replies)
	}
}

type failGitHubThreadOnceStore struct {
	store.Store
	mu     sync.Mutex
	failed bool
}

func (s *failGitHubThreadOnceStore) GetGitHubPullRequestThread(
	ctx context.Context, projectID, repositoryID string, pullNumber int64,
) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.failed {
		s.failed = true
		return "", errors.New("injected GitHub thread lookup failure")
	}
	return s.Store.GetGitHubPullRequestThread(ctx, projectID, repositoryID, pullNumber)
}

func TestGitHubProjectUpdateCoverage(t *testing.T) {
	t.Parallel()
	var payload githubProjectPayload
	payload.Sender.Login = "alice"
	payload.Repository.FullName = "block/buzz"
	payload.PullRequest = &githubPullRequest{Number: 7, Title: "Review me"}

	payload.Action = "closed"
	payload.PullRequest.Merged = true
	updates := githubProjectUpdates("pull_request", payload)
	if len(updates) != 1 || !strings.Contains(updates[0].Body, "merged") {
		t.Fatalf("missing merged PR update: %#v", updates)
	}

	payload.Review = &struct {
		State   string `json:"state"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	}{State: "approved", Body: "Looks good"}
	payload.Review.User.Login = "reviewer"
	updates = githubProjectUpdates("pull_request_review", payload)
	if len(updates) != 1 || !strings.Contains(updates[0].Body, "approved") {
		t.Fatalf("missing review update: %#v", updates)
	}

	payload.Comment = &struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	}{Body: "Please rename this", HTMLURL: "https://github.com/block/buzz/pull/7#discussion"}
	payload.Comment.User.Login = "reviewer"
	updates = githubProjectUpdates("pull_request_review_comment", payload)
	if len(updates) != 1 || !strings.Contains(updates[0].Body, "review comment") || !strings.Contains(updates[0].Body, "View on GitHub") {
		t.Fatalf("missing review comment update: %#v", updates)
	}

	payload.Issue = &githubIssue{Number: 7, PullRequest: &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.test/pulls/7"}}
	payload.Comment = &struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	}{Body: "Please follow up"}
	updates = githubProjectUpdates("issue_comment", payload)
	if len(updates) != 1 || !strings.Contains(updates[0].Body, "Please follow up") {
		t.Fatalf("missing comment update: %#v", updates)
	}

	payload.CheckSuite = &githubCheck{Conclusion: "success"}
	payload.CheckSuite.PullRequests = append(payload.CheckSuite.PullRequests, struct {
		Number int64 `json:"number"`
	}{Number: 7})
	updates = githubProjectUpdates("check_suite", payload)
	if len(updates) != 1 || !strings.Contains(updates[0].Body, "success") {
		t.Fatalf("missing check update: %#v", updates)
	}

	payload.CheckRun = &githubCheck{Name: "lint", Status: "in_progress", HTMLURL: "https://github.com/block/buzz/actions"}
	payload.CheckRun.PullRequests = append(payload.CheckRun.PullRequests, struct {
		Number int64 `json:"number"`
	}{Number: 7})
	updates = githubProjectUpdates("check_run", payload)
	if len(updates) != 1 || !strings.Contains(updates[0].Body, "in progress") || !strings.Contains(updates[0].Body, "View checks on GitHub") {
		t.Fatalf("missing check run update: %#v", updates)
	}

	if updates := githubProjectUpdates("unsupported", payload); len(updates) != 0 {
		t.Fatalf("unsupported event produced updates: %#v", updates)
	}
}

func sendProjectWebhook(t *testing.T, endpoint, secret, eventType, deliveryID string, payload any, wantStatus int) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("webhook: expected %d, got %s %s", wantStatus, resp.Status, string(responseBody))
	}
}
