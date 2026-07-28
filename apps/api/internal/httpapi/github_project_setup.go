package httpapi

import (
	"bytes"
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
	"github.com/oklog/ulid/v2"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"golang.org/x/oauth2"
)

const githubProjectWebhookScope = "admin:repo_hook"

var githubProjectWebhookEvents = []string{
	"check_run",
	"check_suite",
	"issues",
	"issue_comment",
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
}

type githubProjectOAuthDraft struct {
	ProjectID     string                               `json:"project_id"`
	WorkspaceID   string                               `json:"workspace_id"`
	UserID        string                               `json:"user_id"`
	Name          string                               `json:"name"`
	Slug          string                               `json:"slug"`
	Description   string                               `json:"description"`
	WebhookSecret string                               `json:"webhook_secret"`
	Repositories  []store.CreateProjectRepositoryInput `json:"repositories"`
	MemberIDs     []string                             `json:"member_ids"`
}

type githubCreatedHook struct {
	Repository store.CreateProjectRepositoryInput
	ID         int64
}

type githubHookError struct {
	Repository string
	StatusCode int
}

func (e *githubHookError) Error() string {
	return fmt.Sprintf("github webhook request failed for %s with status %d", e.Repository, e.StatusCode)
}

func (s *Server) startGitHubProjectSetup(w http.ResponseWriter, r *http.Request) {
	if s.githubOAuth.ClientID == "" || s.githubOAuth.ClientSecret == "" {
		writeError(w, http.StatusNotImplemented, errors.New("github oauth is not configured"))
		return
	}
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
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
	workspaceID := chi.URLParam(r, "workspace_id")
	workspace, err := s.store.GetWorkspace(r.Context(), workspaceID, act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if workspace.Role != store.WorkspaceRoleOwner && workspace.Role != store.WorkspaceRoleModerator {
		writeError(w, http.StatusForbidden, store.ErrNotWorkspaceManager)
		return
	}
	repositories, err := parseGitHubRepositories(body.Repositories)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	secret, err := newProjectWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	draft := githubProjectOAuthDraft{
		ProjectID:     "prj_" + ulid.Make().String(),
		WorkspaceID:   workspaceID,
		UserID:        act.user.ID,
		Name:          body.Name,
		Slug:          body.Slug,
		Description:   body.Description,
		WebhookSecret: secret,
		Repositories:  repositories,
		MemberIDs:     body.MemberIDs,
	}
	if err := store.ValidateCreateProjectInput(draft.createProjectInput()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	contextJSON, err := json.Marshal(draft)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	redirectURL, err := s.githubRedirectURL(r)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	browserBinding, err := s.oauthBrowserBinding(w, r)
	if err != nil {
		if errors.Is(err, errAmbiguousCookie) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.writeGitHubOAuthServerError(w, r, "project browser binding", err)
		return
	}
	state, err := randomOAuthSecret()
	if err != nil {
		s.writeGitHubOAuthServerError(w, r, "project state generation", err)
		return
	}
	pkceVerifier, err := randomOAuthSecret()
	if err != nil {
		s.writeGitHubOAuthServerError(w, r, "project PKCE generation", err)
		return
	}
	now := time.Now().UTC()
	if err := s.store.CreateOAuthTransaction(r.Context(), store.OAuthTransaction{
		StateHash:          secretHash(state),
		BrowserBindingHash: secretHash(browserBinding),
		Mode:               store.OAuthModeBrowser,
		Purpose:            store.OAuthPurposeProjectWebhook,
		ContextJSON:        string(contextJSON),
		PKCEVerifier:       pkceVerifier,
		CreatedAt:          now,
		ExpiresAt:          now.Add(oauthTransactionTTL),
	}); err != nil {
		if errors.Is(err, store.ErrOAuthCapacityExceeded) {
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		s.writeGitHubOAuthServerError(w, r, "project transaction creation", err)
		return
	}
	authorizationURL := s.oauth2ConfigWithScopes(redirectURL, []string{githubProjectWebhookScope}).AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(pkceVerifier),
	)
	writeJSON(w, http.StatusOK, map[string]string{"authorization_url": authorizationURL})
}

func (draft githubProjectOAuthDraft) createProjectInput() store.CreateProjectInput {
	return store.CreateProjectInput{
		ProjectID:     draft.ProjectID,
		WorkspaceID:   draft.WorkspaceID,
		Name:          draft.Name,
		Slug:          draft.Slug,
		Description:   draft.Description,
		CreatedBy:     draft.UserID,
		WebhookSecret: draft.WebhookSecret,
		Repositories:  draft.Repositories,
		MemberIDs:     draft.MemberIDs,
	}
}

func (s *Server) finishGitHubProjectSetup(
	w http.ResponseWriter,
	r *http.Request,
	transaction store.OAuthTransaction,
	token string,
) {
	var draft githubProjectOAuthDraft
	if err := json.Unmarshal([]byte(transaction.ContextJSON), &draft); err != nil {
		s.redirectGitHubProjectSetup(w, r, transaction, "invalid")
		return
	}
	if err := store.ValidateCreateProjectInput(draft.createProjectInput()); err != nil {
		s.redirectGitHubProjectSetup(w, r, transaction, "invalid")
		return
	}
	act, err := s.currentActor(r)
	if err != nil || act.botTokenID != "" || act.user.ID != draft.UserID {
		s.redirectGitHubProjectSetup(w, r, transaction, "session")
		return
	}
	workspace, err := s.store.GetWorkspace(r.Context(), draft.WorkspaceID, act.user.ID)
	if err != nil || (workspace.Role != store.WorkspaceRoleOwner && workspace.Role != store.WorkspaceRoleModerator) {
		s.redirectGitHubProjectSetup(w, r, transaction, "permission")
		return
	}
	webhookURL := strings.TrimRight(s.apiBaseURL(r), "/") + "/api/hooks/github/projects/" + draft.ProjectID
	createdHooks := make([]githubCreatedHook, 0, len(draft.Repositories))
	for _, repository := range draft.Repositories {
		hookID, err := s.createGitHubRepositoryWebhook(r, token, repository, webhookURL, draft.WebhookSecret)
		if err != nil {
			s.deleteGitHubRepositoryWebhooks(r, token, createdHooks)
			s.redirectGitHubProjectSetup(w, r, transaction, githubProjectSetupErrorCode(err))
			return
		}
		createdHooks = append(createdHooks, githubCreatedHook{Repository: repository, ID: hookID})
	}
	project, event, err := s.store.CreateProject(r.Context(), draft.createProjectInput())
	if err != nil {
		s.deleteGitHubRepositoryWebhooks(r, token, createdHooks)
		s.redirectGitHubProjectSetup(w, r, transaction, "create")
		return
	}
	if event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	for _, hook := range createdHooks {
		_ = s.pingGitHubRepositoryWebhook(r, token, hook)
	}
	destination := "/app/" + url.PathEscape(project.WorkspaceID) + "/" + url.PathEscape(firstNonEmpty(project.Channel.RouteID, project.Channel.ID))
	if s.frontendURL != "" {
		destination = strings.TrimRight(s.frontendURL, "/") + destination
	}
	http.Redirect(w, r, destination, http.StatusFound)
}

func (s *Server) createGitHubRepositoryWebhook(
	r *http.Request,
	token string,
	repository store.CreateProjectRepositoryInput,
	webhookURL string,
	secret string,
) (int64, error) {
	body, err := json.Marshal(map[string]any{
		"name":   "web",
		"active": true,
		"events": githubProjectWebhookEvents,
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "0",
		},
	})
	if err != nil {
		return 0, err
	}
	endpoint := strings.TrimRight(s.githubOAuth.APIURL, "/") + "/repos/" +
		url.PathEscape(repository.Owner) + "/" + url.PathEscape(repository.Name) + "/hooks"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	setGitHubAPIHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return 0, &githubHookError{Repository: repository.FullName, StatusCode: resp.StatusCode}
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&created); err != nil {
		return 0, err
	}
	if created.ID <= 0 {
		return 0, errors.New("github webhook id missing")
	}
	return created.ID, nil
}

func (s *Server) deleteGitHubRepositoryWebhooks(r *http.Request, token string, hooks []githubCreatedHook) {
	for _, hook := range hooks {
		endpoint := strings.TrimRight(s.githubOAuth.APIURL, "/") + "/repos/" +
			url.PathEscape(hook.Repository.Owner) + "/" + url.PathEscape(hook.Repository.Name) +
			"/hooks/" + strconv.FormatInt(hook.ID, 10)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodDelete, endpoint, nil)
		if err != nil {
			continue
		}
		setGitHubAPIHeaders(req, token)
		resp, err := s.githubOAuth.HTTPClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
		}
	}
}

func (s *Server) pingGitHubRepositoryWebhook(r *http.Request, token string, hook githubCreatedHook) error {
	endpoint := strings.TrimRight(s.githubOAuth.APIURL, "/") + "/repos/" +
		url.PathEscape(hook.Repository.Owner) + "/" + url.PathEscape(hook.Repository.Name) +
		"/hooks/" + strconv.FormatInt(hook.ID, 10) + "/pings"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	setGitHubAPIHeaders(req, token)
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusNoContent {
		return &githubHookError{Repository: hook.Repository.FullName, StatusCode: resp.StatusCode}
	}
	return nil
}

func setGitHubAPIHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "ClickClack")
}

func githubProjectSetupErrorCode(err error) string {
	var hookError *githubHookError
	if errors.As(err, &hookError) {
		switch hookError.StatusCode {
		case http.StatusUnauthorized:
			return "authorization"
		case http.StatusForbidden:
			return "permission"
		case http.StatusNotFound:
			return "repository"
		case http.StatusUnprocessableEntity:
			return "webhook_conflict"
		}
	}
	return "github"
}

func (s *Server) redirectGitHubProjectSetup(
	w http.ResponseWriter,
	r *http.Request,
	transaction store.OAuthTransaction,
	reason string,
) {
	var draft githubProjectOAuthDraft
	_ = json.Unmarshal([]byte(transaction.ContextJSON), &draft)
	destination := "/app/" + url.PathEscape(draft.WorkspaceID) + "/projects"
	query := url.Values{"github_setup": []string{"error"}, "reason": []string{reason}}
	destination += "?" + query.Encode()
	if s.frontendURL != "" {
		destination = strings.TrimRight(s.frontendURL, "/") + destination
	}
	http.Redirect(w, r, destination, http.StatusFound)
}
