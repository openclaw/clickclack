package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type createProjectRequest struct {
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Description  string   `json:"description"`
	Repositories []string `json:"repositories"`
	MemberIDs    []string `json:"member_ids"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("workspaces:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	projects, err := s.store.ListProjects(r.Context(), workspaceID, act.user.ID)
	writeResult(w, map[string]any{"projects": projects}, err)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	s.writeProject(w, r, false)
}

func (s *Server) getProjectContext(w http.ResponseWriter, r *http.Request) {
	s.writeProject(w, r, true)
}

func (s *Server) writeProject(w http.ResponseWriter, r *http.Request, contextResponse bool) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("workspaces:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	project, err := s.store.GetProject(r.Context(), chi.URLParam(r, "project_id"), act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := act.requireWorkspace(project.WorkspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if contextResponse {
		writeJSON(w, http.StatusOK, map[string]any{
			"project": project,
			"context": map[string]any{
				"source_of_truth": "github",
				"write_access":    false,
				"channel_id":      project.Channel.ID,
				"repositories":    project.Repositories,
				"participants":    project.Members,
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("workspaces:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create projects"))
		return
	}
	var body createProjectRequest
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repositories, err := parseGitHubRepositories(body.Repositories)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	projectName := strings.TrimSpace(body.Name)
	if projectName == "" && len(repositories) > 0 {
		projectName = repositories[0].Name
	}
	secret, err := newProjectWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	project, event, err := s.store.CreateProject(r.Context(), store.CreateProjectInput{
		WorkspaceID:   workspaceID,
		Name:          projectName,
		Slug:          body.Slug,
		Description:   body.Description,
		CreatedBy:     act.user.ID,
		WebhookSecret: secret,
		Repositories:  repositories,
		MemberIDs:     body.MemberIDs,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	webhookURL := strings.TrimRight(s.apiBaseURL(r), "/") + "/api/hooks/github/projects/" + project.ID
	writeJSON(w, http.StatusCreated, map[string]any{
		"project": project,
		"webhook": map[string]string{"url": webhookURL, "secret": secret},
	})
}

func parseGitHubRepositories(rawRepositories []string) ([]store.CreateProjectRepositoryInput, error) {
	repositories := make([]store.CreateProjectRepositoryInput, 0, len(rawRepositories))
	seen := make(map[string]struct{}, len(rawRepositories))
	for _, raw := range rawRepositories {
		repository, err := parseGitHubRepository(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[repository.FullName]; ok {
			continue
		}
		seen[repository.FullName] = struct{}{}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

func parseGitHubRepository(raw string) (store.CreateProjectRepositoryInput, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return store.CreateProjectRepositoryInput{}, errors.New("GitHub repository is required")
	}
	if !strings.Contains(value, "://") {
		value = "https://github.com/" + strings.TrimPrefix(value, "/")
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return store.CreateProjectRepositoryInput{}, fmt.Errorf("%q is not a GitHub repository URL", raw)
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return store.CreateProjectRepositoryInput{}, fmt.Errorf("%q must identify one GitHub repository", raw)
	}
	owner := strings.ToLower(parts[0])
	name := strings.ToLower(parts[1])
	return store.CreateProjectRepositoryInput{
		Owner: owner, Name: name, FullName: owner + "/" + name,
		URL: "https://github.com/" + owner + "/" + name,
	}, nil
}

func newProjectWebhookSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "ccgh_" + base64.RawURLEncoding.EncodeToString(value), nil
}

type githubProjectPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	PullRequest *githubPullRequest `json:"pull_request"`
	Review      *struct {
		State   string `json:"state"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
	Comment *struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue      *githubIssue `json:"issue"`
	CheckSuite *githubCheck `json:"check_suite"`
	CheckRun   *githubCheck `json:"check_run"`
}

type githubPullRequest struct {
	Number  int64  `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Draft   bool   `json:"draft"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type githubIssue struct {
	Number  int64  `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

type githubCheck struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HTMLURL      string `json:"html_url"`
	HeadBranch   string `json:"head_branch"`
	PullRequests []struct {
		Number int64 `json:"number"`
	} `json:"pull_requests"`
}

func (s *Server) githubProjectWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	payloadBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload githubProjectPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid GitHub webhook payload"))
		return
	}
	target, err := s.store.GetGitHubWebhookTarget(r.Context(), chi.URLParam(r, "project_id"), payload.Repository.FullName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid GitHub webhook target"))
		return
	}
	if !validGitHubSignature(payloadBytes, r.Header.Get("X-Hub-Signature-256"), target.WebhookSecret) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid GitHub webhook signature"))
		return
	}
	eventType := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if eventType == "ping" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	if deliveryID == "" || eventType == "" {
		writeError(w, http.StatusBadRequest, errors.New("GitHub delivery and event headers are required"))
		return
	}
	claim, err := s.store.ClaimGitHubDelivery(r.Context(), target.ProjectID, deliveryID, eventType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	switch claim {
	case store.GitHubDeliveryComplete:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "duplicate", "delivery_id": deliveryID})
		return
	case store.GitHubDeliveryProcessing:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, errors.New("GitHub delivery is still processing"))
		return
	case store.GitHubDeliveryClaimed:
	default:
		writeError(w, http.StatusInternalServerError, errors.New("invalid GitHub delivery claim state"))
		return
	}
	completed := false
	defer func() {
		if !completed {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			_ = s.store.FailGitHubDelivery(cleanupCtx, target.ProjectID, deliveryID)
		}
	}()

	updates := githubProjectUpdates(eventType, payload)
	for _, update := range updates {
		if err := s.postGitHubProjectUpdate(r, target, deliveryID, update); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := s.store.CompleteGitHubDelivery(r.Context(), target.ProjectID, deliveryID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	completed = true
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "accepted", "delivery_id": deliveryID, "updates": len(updates),
	})
}

func validGitHubSignature(payload []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hmac.Equal(provided, mac.Sum(nil))
}

type githubProjectUpdate struct {
	Number   int64
	Root     githubPullRequest
	Issue    *githubIssue
	Body     string
	RootOnly bool
}

func githubProjectUpdates(eventType string, payload githubProjectPayload) []githubProjectUpdate {
	actor := firstNonEmpty(payload.Sender.Login, "GitHub")
	switch eventType {
	case "pull_request":
		if payload.PullRequest == nil {
			return nil
		}
		action := strings.ReplaceAll(payload.Action, "_", " ")
		body := fmt.Sprintf("**%s** %s pull request #%d.", actor, action, payload.PullRequest.Number)
		if payload.Action == "synchronize" {
			body = fmt.Sprintf("**%s** pushed new commits to pull request #%d.", actor, payload.PullRequest.Number)
		} else if payload.Action == "closed" && payload.PullRequest.Merged {
			body = fmt.Sprintf("**%s** merged pull request #%d.", actor, payload.PullRequest.Number)
		}
		return []githubProjectUpdate{{
			Number: payload.PullRequest.Number, Root: *payload.PullRequest, Body: body,
			RootOnly: payload.Action == "opened",
		}}
	case "issues":
		if payload.Issue == nil || payload.Issue.PullRequest != nil {
			return nil
		}
		action := strings.ReplaceAll(payload.Action, "_", " ")
		body := fmt.Sprintf("**%s** %s issue #%d.", actor, action, payload.Issue.Number)
		return []githubProjectUpdate{{
			Number:   payload.Issue.Number,
			Issue:    payload.Issue,
			Body:     body,
			RootOnly: payload.Action == "opened",
		}}
	case "pull_request_review":
		if payload.PullRequest == nil || payload.Review == nil {
			return nil
		}
		reviewer := firstNonEmpty(payload.Review.User.Login, actor)
		body := fmt.Sprintf("**%s** submitted a **%s** review.", reviewer, strings.ToLower(payload.Review.State))
		if excerpt := webhookExcerpt(payload.Review.Body); excerpt != "" {
			body += "\n\n> " + strings.ReplaceAll(excerpt, "\n", "\n> ")
		}
		if payload.Review.HTMLURL != "" {
			body += "\n\n[View review on GitHub](" + payload.Review.HTMLURL + ")"
		}
		return []githubProjectUpdate{{Number: payload.PullRequest.Number, Root: *payload.PullRequest, Body: body}}
	case "pull_request_review_comment":
		if payload.PullRequest == nil || payload.Comment == nil {
			return nil
		}
		return []githubProjectUpdate{{
			Number: payload.PullRequest.Number, Root: *payload.PullRequest,
			Body: githubCommentBody(firstNonEmpty(payload.Comment.User.Login, actor), "left a review comment", payload.Comment.Body, payload.Comment.HTMLURL),
		}}
	case "issue_comment":
		if payload.Issue == nil || payload.Comment == nil {
			return nil
		}
		if payload.Issue.PullRequest == nil {
			return []githubProjectUpdate{{
				Number: payload.Issue.Number,
				Issue:  payload.Issue,
				Body: githubCommentBody(
					firstNonEmpty(payload.Comment.User.Login, actor),
					"commented",
					payload.Comment.Body,
					payload.Comment.HTMLURL,
				),
			}}
		}
		root := githubPullRequest{Number: payload.Issue.Number, HTMLURL: payload.Issue.HTMLURL}
		return []githubProjectUpdate{{
			Number: payload.Issue.Number, Root: root,
			Body: githubCommentBody(firstNonEmpty(payload.Comment.User.Login, actor), "commented", payload.Comment.Body, payload.Comment.HTMLURL),
		}}
	case "check_suite", "check_run":
		check := payload.CheckSuite
		if eventType == "check_run" {
			check = payload.CheckRun
		}
		if check == nil {
			return nil
		}
		status := firstNonEmpty(check.Conclusion, check.Status, payload.Action)
		label := firstNonEmpty(check.Name, "CI checks")
		body := fmt.Sprintf("**%s**: %s.", label, strings.ReplaceAll(status, "_", " "))
		if check.HTMLURL != "" {
			body += "\n\n[View checks on GitHub](" + check.HTMLURL + ")"
		}
		updates := make([]githubProjectUpdate, 0, len(check.PullRequests))
		for _, pull := range check.PullRequests {
			updates = append(updates, githubProjectUpdate{
				Number: pull.Number, Body: body,
			})
		}
		return updates
	default:
		return nil
	}
}

func (s *Server) postGitHubProjectUpdate(r *http.Request, target store.GitHubWebhookTarget, deliveryID string, update githubProjectUpdate) error {
	rootID, err := s.store.GetGitHubPullRequestThread(r.Context(), target.ProjectID, target.RepositoryID, update.Number)
	if errors.Is(err, sql.ErrNoRows) {
		rootBody := githubProjectRootBody(target.RepositoryFullName, update)
		message, event, createErr := s.store.CreateMessage(r.Context(), store.CreateMessageInput{
			ChannelID: target.ChannelID, AuthorID: target.IntegrationUserID, Body: rootBody,
			Nonce: githubNonce("pr", target.ProjectID, target.RepositoryID, strconv.FormatInt(update.Number, 10)),
		})
		if createErr != nil {
			return createErr
		}
		if event.ID != "" {
			s.publishEvent(r.Context(), event)
			s.notifyMessageCreated(r.Context(), message)
		}
		rootID, err = s.store.SetGitHubPullRequestThread(r.Context(), target.ProjectID, target.RepositoryID, update.Number, message.ID)
		if err != nil {
			return err
		}
		if update.RootOnly {
			return nil
		}
	} else if err != nil {
		return err
	} else if update.RootOnly {
		rootBody := githubProjectRootBody(target.RepositoryFullName, update)
		message, getErr := s.store.GetMessage(r.Context(), rootID, target.IntegrationUserID)
		if getErr != nil {
			return getErr
		}
		if message.Body != rootBody {
			_, event, updateErr := s.store.UpdateMessage(r.Context(), store.UpdateMessageInput{
				MessageID: rootID, UserID: target.IntegrationUserID, Body: rootBody,
			})
			if updateErr != nil {
				return updateErr
			}
			if event.ID != "" {
				s.publishEvent(r.Context(), event)
			}
		}
		return nil
	}
	reply, _, events, err := s.store.CreateThreadReply(r.Context(), store.CreateThreadReplyInput{
		RootMessageID: rootID, AuthorID: target.IntegrationUserID, Body: update.Body,
		Nonce: githubNonce("delivery", target.ProjectID, deliveryID, strconv.FormatInt(update.Number, 10)),
	})
	if err != nil {
		return err
	}
	if len(events) > 0 {
		s.publishEvents(r.Context(), events)
		s.notifyMessageCreated(r.Context(), reply)
	}
	return nil
}

func githubProjectRootBody(repositoryFullName string, update githubProjectUpdate) string {
	if update.Issue != nil {
		return githubIssueRootBody(repositoryFullName, *update.Issue)
	}
	return githubPullRequestRootBody(repositoryFullName, update)
}

func githubPullRequestRootBody(repositoryFullName string, update githubProjectUpdate) string {
	title := strings.TrimSpace(update.Root.Title)
	if title == "" {
		title = "Pull request #" + strconv.FormatInt(update.Number, 10)
	}
	body := fmt.Sprintf("**PR #%d · %s**\n%s", update.Number, title, repositoryFullName)
	if update.Root.User.Login != "" {
		body += " · opened by **" + update.Root.User.Login + "**"
	}
	if update.Root.Head.Ref != "" || update.Root.Base.Ref != "" {
		body += "\n`" + update.Root.Head.Ref + "` → `" + update.Root.Base.Ref + "`"
	}
	if update.Root.HTMLURL != "" {
		body += "\n\n[View pull request on GitHub](" + update.Root.HTMLURL + ")"
	}
	return body
}

func githubIssueRootBody(repositoryFullName string, issue githubIssue) string {
	title := strings.TrimSpace(issue.Title)
	if title == "" {
		title = "Issue #" + strconv.FormatInt(issue.Number, 10)
	}
	body := fmt.Sprintf("**Issue #%d · %s**\n%s", issue.Number, title, repositoryFullName)
	if issue.User.Login != "" {
		body += " · opened by **" + issue.User.Login + "**"
	}
	if excerpt := webhookExcerpt(issue.Body); excerpt != "" {
		body += "\n\n> " + strings.ReplaceAll(excerpt, "\n", "\n> ")
	}
	if issue.HTMLURL != "" {
		body += "\n\n[View issue on GitHub](" + issue.HTMLURL + ")"
	}
	return body
}

func githubCommentBody(actor, action, body, link string) string {
	result := fmt.Sprintf("**%s** %s.", actor, action)
	if excerpt := webhookExcerpt(body); excerpt != "" {
		result += "\n\n> " + strings.ReplaceAll(excerpt, "\n", "\n> ")
	}
	if link != "" {
		result += "\n\n[View on GitHub](" + link + ")"
	}
	return result
}

func webhookExcerpt(value string) string {
	value = strings.TrimSpace(value)
	const maxRunes = 800
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return value
}

func githubNonce(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "gh_" + hex.EncodeToString(sum[:16])
}
