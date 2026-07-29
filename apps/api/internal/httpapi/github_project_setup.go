package httpapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

const (
	githubProjectRepositoryScope      = "repo"
	githubProjectSetupGrantTTL        = 10 * time.Minute
	githubProjectSetupGrantVersion    = 2
	githubProjectSetupMaxMembers      = 50
	githubProjectSetupMaxMemberID     = 128
	githubRepositoryPageSize          = 100
	githubRepositoryMaxPages          = 10
	githubProjectSetupCookieMax       = 4096
	githubTokenRevocationRetryDelay   = time.Minute
	githubTokenRevocationRestoreLimit = 8192
	githubTokenRevocationRestoreDelay = 10 * time.Millisecond
	githubWebhookRollbackAttempts     = 3
	githubWebhookRollbackBackoff      = 100 * time.Millisecond
)

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

type githubProjectSetupRequest struct {
	Description string   `json:"description"`
	MemberIDs   []string `json:"member_ids"`
}

type githubProjectSetupGrant struct {
	Version            int                     `json:"version"`
	AccessToken        string                  `json:"access_token"`
	RevocationID       string                  `json:"revocation_id"`
	BrowserBindingHash string                  `json:"browser_binding_hash"`
	Draft              githubProjectOAuthDraft `json:"draft"`
	IssuedAt           int64                   `json:"issued_at"`
	ExpiresAt          int64                   `json:"expires_at"`
}

type githubProjectRepositoryOption struct {
	FullName    string `json:"full_name"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	Private     bool   `json:"private"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type githubProjectSetupCompleteRequest struct {
	Repositories []string `json:"repositories"`
}

type githubCreatedHook struct {
	Repository store.CreateProjectRepositoryInput
	ID         int64
}

type githubProjectRevocationJob struct {
	timer *time.Timer
	runAt time.Time
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
	var body githubProjectSetupRequest
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
	secret, err := newProjectWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	draft := githubProjectOAuthDraft{
		ProjectID:     "prj_" + ulid.Make().String(),
		WorkspaceID:   workspaceID,
		UserID:        act.user.ID,
		Description:   body.Description,
		WebhookSecret: secret,
		MemberIDs:     body.MemberIDs,
	}
	if err := validateGitHubProjectDraft(draft); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	contextJSON, err := json.Marshal(draft)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(contextJSON) > store.MaxOAuthTransactionContextBytes {
		writeError(w, http.StatusBadRequest, errors.New("project setup details are too large"))
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
	authorizationURL := s.oauth2ConfigWithScopes(redirectURL, []string{githubProjectRepositoryScope}).AuthCodeURL(
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

func validateGitHubProjectDraft(draft githubProjectOAuthDraft) error {
	description := strings.TrimSpace(draft.Description)
	if len([]rune(description)) > 500 {
		return errors.New("project description must be 500 characters or fewer")
	}
	if draft.ProjectID == "" || draft.WorkspaceID == "" || draft.UserID == "" || draft.WebhookSecret == "" {
		return errors.New("project setup is incomplete")
	}
	if len(draft.MemberIDs) > githubProjectSetupMaxMembers {
		return fmt.Errorf("automatic GitHub setup supports at most %d participants", githubProjectSetupMaxMembers)
	}
	for _, memberID := range draft.MemberIDs {
		if memberID == "" || len(memberID) > githubProjectSetupMaxMemberID {
			return errors.New("project participant id is invalid")
		}
	}
	return nil
}

func (s *Server) finishGitHubProjectSetup(
	w http.ResponseWriter,
	r *http.Request,
	transaction store.OAuthTransaction,
	token string,
) {
	revocation, err := s.createPendingGitHubTokenRevocation(r.Context(), token)
	if err != nil {
		s.revokeGitHubProjectSetupTokenOrRetry(r.Context(), "", token, "queue")
		s.redirectGitHubProjectSetup(w, r, transaction, "server")
		return
	}
	revokeAndRedirect := func(code string) {
		s.revokeGitHubProjectSetupTokenOrRetry(r.Context(), revocation.ID, token, code)
		s.redirectGitHubProjectSetup(w, r, transaction, code)
	}
	var draft githubProjectOAuthDraft
	if err := json.Unmarshal([]byte(transaction.ContextJSON), &draft); err != nil {
		revokeAndRedirect("invalid")
		return
	}
	if err := validateGitHubProjectDraft(draft); err != nil {
		revokeAndRedirect("invalid")
		return
	}
	act, err := s.currentActor(r)
	if err != nil || act.botTokenID != "" || act.user.ID != draft.UserID {
		revokeAndRedirect("session")
		return
	}
	workspace, err := s.store.GetWorkspace(r.Context(), draft.WorkspaceID, act.user.ID)
	if err != nil || (workspace.Role != store.WorkspaceRoleOwner && workspace.Role != store.WorkspaceRoleModerator) {
		revokeAndRedirect("permission")
		return
	}
	now := time.Now().UTC()
	grant := githubProjectSetupGrant{
		Version:            githubProjectSetupGrantVersion,
		AccessToken:        token,
		RevocationID:       revocation.ID,
		BrowserBindingHash: transaction.BrowserBindingHash,
		Draft:              draft,
		IssuedAt:           now.Unix(),
		ExpiresAt:          now.Add(githubProjectSetupGrantTTL).Unix(),
	}
	if err := s.setGitHubProjectSetupGrant(w, r, grant); err != nil {
		revokeAndRedirect("session")
		return
	}
	s.scheduleGitHubProjectSetupTokenRevocation(revocation.ID, token, time.Until(revocation.RevokeAfter))
	destination := "/app/" + url.PathEscape(draft.WorkspaceID) + "/projects?github_setup=select"
	if s.frontendURL != "" {
		destination = strings.TrimRight(s.frontendURL, "/") + destination
	}
	http.Redirect(w, r, destination, http.StatusFound)
}

func (s *Server) listGitHubProjectRepositories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	grant, err := s.authorizedGitHubProjectSetupGrant(r, chi.URLParam(r, "workspace_id"))
	if err != nil {
		s.clearGitHubProjectSetupGrant(w, r)
		writeError(w, http.StatusUnauthorized, errors.New("GitHub project setup expired; connect GitHub again"))
		return
	}
	repositories, truncated, err := s.fetchGitHubProjectRepositories(r, grant.AccessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, errors.New("GitHub repositories could not be loaded"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"setup": map[string]any{
			"description": grant.Draft.Description,
			"expires_at":  time.Unix(grant.ExpiresAt, 0).UTC().Format(time.RFC3339),
		},
		"repositories": repositories,
		"truncated":    truncated,
	})
}

func (s *Server) completeGitHubProjectSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	grant, err := s.authorizedGitHubProjectSetupGrant(r, chi.URLParam(r, "workspace_id"))
	if err != nil {
		s.clearGitHubProjectSetupGrant(w, r)
		writeError(w, http.StatusUnauthorized, errors.New("GitHub project setup expired; connect GitHub again"))
		return
	}
	var body githubProjectSetupCompleteRequest
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repositories, err := parseGitHubRepositories(body.Repositories)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(repositories) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("select at least one GitHub repository"))
		return
	}
	available, _, err := s.fetchGitHubProjectRepositories(r, grant.AccessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, errors.New("GitHub repository access could not be verified"))
		return
	}
	adminRepositories := make(map[string]githubProjectRepositoryOption, len(available))
	for _, repository := range available {
		adminRepositories[strings.ToLower(repository.FullName)] = repository
	}
	for _, repository := range repositories {
		if _, ok := adminRepositories[repository.FullName]; !ok {
			writeError(w, http.StatusForbidden, fmt.Errorf("GitHub webhook access is not available for %s", repository.FullName))
			return
		}
	}
	primaryRepository := adminRepositories[repositories[0].FullName]
	grant.Draft.Name = strings.TrimSpace(primaryRepository.Name)
	grant.Draft.Slug = strings.TrimSpace(primaryRepository.FullName)
	grant.Draft.Repositories = repositories
	if err := store.ValidateCreateProjectInput(grant.Draft.createProjectInput()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	webhookURL := strings.TrimRight(s.apiBaseURL(r), "/") + "/api/hooks/github/projects/" + grant.Draft.ProjectID
	createdHooks := make([]githubCreatedHook, 0, len(repositories))
	for _, repository := range repositories {
		hookID, err := s.createGitHubRepositoryWebhook(r, grant.AccessToken, repository, webhookURL, grant.Draft.WebhookSecret)
		if err != nil {
			s.rollbackGitHubRepositoryWebhooks(r, grant.AccessToken, createdHooks)
			writeError(w, http.StatusBadGateway, errors.New("GitHub could not create all repository webhooks"))
			return
		}
		createdHooks = append(createdHooks, githubCreatedHook{Repository: repository, ID: hookID})
	}
	project, event, err := s.store.CreateProject(r.Context(), grant.Draft.createProjectInput())
	if err != nil {
		s.rollbackGitHubRepositoryWebhooks(r, grant.AccessToken, createdHooks)
		writeStoreError(w, err)
		return
	}
	s.clearGitHubProjectSetupGrant(w, r)
	if event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	for _, hook := range createdHooks {
		_ = s.pingGitHubRepositoryWebhook(r, grant.AccessToken, hook)
	}
	s.revokeGitHubProjectSetupTokenOrRetry(r.Context(), grant.RevocationID, grant.AccessToken, "complete")
	writeJSON(w, http.StatusCreated, map[string]any{"project": project})
}

func (s *Server) cancelGitHubProjectSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	grant, err := s.authorizedGitHubProjectSetupGrant(r, chi.URLParam(r, "workspace_id"))
	if err != nil {
		s.clearGitHubProjectSetupGrant(w, r)
		writeError(w, http.StatusUnauthorized, errors.New("GitHub project setup expired"))
		return
	}
	s.revokeGitHubProjectSetupTokenOrRetry(r.Context(), grant.RevocationID, grant.AccessToken, "cancel")
	s.clearGitHubProjectSetupGrant(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizedGitHubProjectSetupGrant(r *http.Request, workspaceID string) (githubProjectSetupGrant, error) {
	grant, err := s.githubProjectSetupGrant(r)
	if err != nil {
		return githubProjectSetupGrant{}, err
	}
	act, err := s.currentActor(r)
	if err != nil || act.botTokenID != "" || act.user.ID != grant.Draft.UserID || workspaceID != grant.Draft.WorkspaceID {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup does not match this session")
	}
	workspace, err := s.store.GetWorkspace(r.Context(), workspaceID, act.user.ID)
	if err != nil || (workspace.Role != store.WorkspaceRoleOwner && workspace.Role != store.WorkspaceRoleModerator) {
		return githubProjectSetupGrant{}, store.ErrNotWorkspaceManager
	}
	return grant, nil
}

func (s *Server) fetchGitHubProjectRepositories(
	r *http.Request,
	token string,
) ([]githubProjectRepositoryOption, bool, error) {
	repositories := make([]githubProjectRepositoryOption, 0, githubRepositoryPageSize)
	seen := make(map[string]struct{})
	truncated := false
	for page := 1; page <= githubRepositoryMaxPages; page++ {
		endpoint, err := url.Parse(strings.TrimRight(s.githubOAuth.APIURL, "/") + "/user/repos")
		if err != nil {
			return nil, false, err
		}
		query := endpoint.Query()
		query.Set("affiliation", "owner,collaborator,organization_member")
		query.Set("sort", "updated")
		query.Set("per_page", strconv.Itoa(githubRepositoryPageSize))
		query.Set("page", strconv.Itoa(page))
		endpoint.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, false, err
		}
		setGitHubAPIHeaders(req, token)
		resp, err := s.githubOAuth.HTTPClient.Do(req)
		if err != nil {
			return nil, false, err
		}
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			return nil, false, fmt.Errorf("GitHub repository request failed with status %d", resp.StatusCode)
		}
		var pageRepositories []struct {
			Name        string `json:"name"`
			FullName    string `json:"full_name"`
			Private     bool   `json:"private"`
			HTMLURL     string `json:"html_url"`
			Description string `json:"description"`
			UpdatedAt   string `json:"updated_at"`
			Owner       struct {
				Login string `json:"login"`
			} `json:"owner"`
			Permissions struct {
				Admin bool `json:"admin"`
			} `json:"permissions"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&pageRepositories)
		_ = resp.Body.Close()
		if err != nil {
			return nil, false, err
		}
		for _, repository := range pageRepositories {
			fullName := strings.ToLower(strings.TrimSpace(repository.FullName))
			if !repository.Permissions.Admin || fullName == "" {
				continue
			}
			if _, ok := seen[fullName]; ok {
				continue
			}
			seen[fullName] = struct{}{}
			repositories = append(repositories, githubProjectRepositoryOption{
				FullName:    fullName,
				Name:        repository.Name,
				Owner:       repository.Owner.Login,
				Private:     repository.Private,
				HTMLURL:     repository.HTMLURL,
				Description: repository.Description,
				UpdatedAt:   repository.UpdatedAt,
			})
		}
		if len(pageRepositories) < githubRepositoryPageSize {
			break
		}
		if page == githubRepositoryMaxPages {
			truncated = true
		}
	}
	return repositories, truncated, nil
}

func (s *Server) setGitHubProjectSetupGrant(w http.ResponseWriter, r *http.Request, grant githubProjectSetupGrant) error {
	value, err := s.sealGitHubProjectSetupGrant(grant)
	if err != nil {
		return err
	}
	if len(s.cookies.GitHubProjectSetup)+len(value) > githubProjectSetupCookieMax {
		return errors.New("GitHub project setup is too large")
	}
	expires := time.Unix(grant.ExpiresAt, 0).UTC()
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookies.GitHubProjectSetup,
		Value:    value,
		Path:     s.cookiePath(),
		MaxAge:   int(time.Until(expires).Seconds()),
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: s.cookieSameSite,
	})
	return nil
}

func (s *Server) clearGitHubProjectSetupGrant(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookies.GitHubProjectSetup,
		Value:    "",
		Path:     s.cookiePath(),
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: s.cookieSameSite,
	})
}

func (s *Server) sealGitHubProjectSetupGrant(grant githubProjectSetupGrant) (string, error) {
	plaintext, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	gcm, err := s.githubProjectSetupAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, s.githubProjectSetupCookieAAD())
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Server) githubProjectSetupGrant(r *http.Request) (githubProjectSetupGrant, error) {
	cookie, err := requestCookie(r, s.cookies.GitHubProjectSetup)
	if err != nil || cookie.Value == "" {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie is missing")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie is invalid")
	}
	gcm, err := s.githubProjectSetupAEAD()
	if err != nil || len(encoded) < gcm.NonceSize() {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie is invalid")
	}
	plaintext, err := gcm.Open(
		nil,
		encoded[:gcm.NonceSize()],
		encoded[gcm.NonceSize():],
		s.githubProjectSetupCookieAAD(),
	)
	if err != nil {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie is invalid")
	}
	var grant githubProjectSetupGrant
	if err := json.Unmarshal(plaintext, &grant); err != nil {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie is invalid")
	}
	now := time.Now().UTC().Unix()
	if grant.Version != githubProjectSetupGrantVersion || grant.AccessToken == "" || grant.RevocationID == "" ||
		grant.BrowserBindingHash == "" || grant.ExpiresAt <= now || grant.IssuedAt > now {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie expired")
	}
	bindingCookie, err := requestCookie(r, s.cookies.OAuthBinding)
	if err != nil || secretHash(bindingCookie.Value) != grant.BrowserBindingHash {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup browser binding changed")
	}
	if err := validateGitHubProjectDraft(grant.Draft); err != nil {
		return githubProjectSetupGrant{}, errors.New("GitHub project setup cookie is invalid")
	}
	return grant, nil
}

func (s *Server) githubProjectSetupAEAD() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("clickclack/github-project-setup/v1\x00" + s.githubOAuth.ClientSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Server) githubProjectSetupCookieAAD() []byte {
	return []byte("clickclack/github-project-setup/v1\x00" + s.cookies.GitHubProjectSetup)
}

func (s *Server) createPendingGitHubTokenRevocation(
	ctx context.Context,
	token string,
) (store.PendingGitHubTokenRevocation, error) {
	encryptedToken, err := s.sealGitHubProjectSetupToken(token)
	if err != nil {
		return store.PendingGitHubTokenRevocation{}, err
	}
	now := time.Now().UTC()
	revocation := store.PendingGitHubTokenRevocation{
		ID:             "gtr_" + ulid.Make().String(),
		EncryptedToken: encryptedToken,
		RevokeAfter:    now.Add(githubProjectSetupGrantTTL),
		CreatedAt:      now,
	}
	if err := s.store.CreatePendingGitHubTokenRevocation(ctx, revocation); err != nil {
		return store.PendingGitHubTokenRevocation{}, err
	}
	return revocation, nil
}

func (s *Server) sealGitHubProjectSetupToken(token string) (string, error) {
	gcm, err := s.githubProjectSetupAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(token), s.githubProjectSetupTokenAAD())
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Server) unsealGitHubProjectSetupToken(value string) (string, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	gcm, err := s.githubProjectSetupAEAD()
	if err != nil || len(encoded) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted GitHub token")
	}
	plaintext, err := gcm.Open(
		nil,
		encoded[:gcm.NonceSize()],
		encoded[gcm.NonceSize():],
		s.githubProjectSetupTokenAAD(),
	)
	if err != nil || len(plaintext) == 0 {
		return "", errors.New("invalid encrypted GitHub token")
	}
	return string(plaintext), nil
}

func (s *Server) githubProjectSetupTokenAAD() []byte {
	return []byte("clickclack/github-project-token-revocation/v1")
}

func (s *Server) restorePendingGitHubTokenRevocations() {
	s.restorePendingGitHubTokenRevocationsWithLimit(githubTokenRevocationRestoreLimit)
}

func (s *Server) restorePendingGitHubTokenRevocationsWithLimit(limit int) {
	if s.githubOAuth.ClientID == "" || s.githubOAuth.ClientSecret == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultGitHubHTTPTimeout)
		defer cancel()
		revocations, err := s.store.ListPendingGitHubTokenRevocations(ctx, limit)
		if err != nil {
			log.Printf("github token revocation restore failed error_type=%T", err)
			return
		}
		for _, revocation := range revocations {
			token, err := s.unsealGitHubProjectSetupToken(revocation.EncryptedToken)
			if err != nil {
				log.Printf("github token revocation decrypt failed revocation_id=%q error_type=%T", revocation.ID, err)
				continue
			}
			s.scheduleGitHubProjectSetupTokenRevocation(
				revocation.ID,
				token,
				time.Until(revocation.RevokeAfter),
			)
		}
	}()
}

func (s *Server) scheduleGitHubProjectSetupTokenRevocation(revocationID, token string, delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	if revocationID == "" {
		time.AfterFunc(delay, func() {
			s.revokeGitHubProjectSetupTokenOrRetry(context.Background(), revocationID, token, "scheduled")
		})
		return
	}

	runAt := time.Now().Add(delay)
	s.githubRevocationMu.Lock()
	if s.githubRevocationJobs == nil {
		s.githubRevocationJobs = make(map[string]githubProjectRevocationJob)
	}
	if existing, ok := s.githubRevocationJobs[revocationID]; ok {
		if !runAt.Before(existing.runAt) {
			s.githubRevocationMu.Unlock()
			return
		}
		existing.timer.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		s.githubRevocationMu.Lock()
		if current, ok := s.githubRevocationJobs[revocationID]; ok && current.timer == timer {
			delete(s.githubRevocationJobs, revocationID)
		}
		s.githubRevocationMu.Unlock()
		s.revokeGitHubProjectSetupTokenOrRetry(context.Background(), revocationID, token, "scheduled")
	})
	s.githubRevocationJobs[revocationID] = githubProjectRevocationJob{timer: timer, runAt: runAt}
	s.githubRevocationMu.Unlock()
}

func (s *Server) clearGitHubProjectSetupTokenRevocation(revocationID string) {
	if revocationID == "" {
		return
	}
	s.githubRevocationMu.Lock()
	if job, ok := s.githubRevocationJobs[revocationID]; ok {
		job.timer.Stop()
		delete(s.githubRevocationJobs, revocationID)
	}
	s.githubRevocationMu.Unlock()
}

func (s *Server) queuePendingGitHubTokenRevocationRestore() {
	if s.githubOAuth.ClientID == "" || s.githubOAuth.ClientSecret == "" {
		return
	}
	s.githubRevocationMu.Lock()
	if s.githubRestoreQueued {
		s.githubRevocationMu.Unlock()
		return
	}
	s.githubRestoreQueued = true
	s.githubRevocationMu.Unlock()
	time.AfterFunc(githubTokenRevocationRestoreDelay, func() {
		s.githubRevocationMu.Lock()
		s.githubRestoreQueued = false
		s.githubRevocationMu.Unlock()
		s.restorePendingGitHubTokenRevocations()
	})
}

func (s *Server) revokeGitHubProjectSetupTokenOrRetry(
	parent context.Context,
	revocationID string,
	token string,
	reason string,
) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), defaultGitHubHTTPTimeout)
	err := s.revokeGitHubProjectSetupToken(ctx, token)
	deleted := false
	if err == nil && revocationID != "" {
		err = s.store.DeletePendingGitHubTokenRevocation(ctx, revocationID)
		deleted = err == nil
	}
	cancel()
	if err == nil {
		if deleted {
			s.clearGitHubProjectSetupTokenRevocation(revocationID)
			s.queuePendingGitHubTokenRevocationRestore()
		}
		return
	}
	log.Printf(
		"github token revocation deferred revocation_id=%q reason=%q error_type=%T",
		revocationID,
		reason,
		err,
	)
	s.scheduleGitHubProjectSetupTokenRevocation(
		revocationID,
		token,
		githubTokenRevocationRetryDelay,
	)
}

func (s *Server) revokeGitHubProjectSetupToken(ctx context.Context, token string) error {
	body, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(s.githubOAuth.APIURL, "/") + "/applications/" +
		url.PathEscape(s.githubOAuth.ClientID) + "/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.githubOAuth.ClientID, s.githubOAuth.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "ClickClack")
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("GitHub token revocation failed with status %d", resp.StatusCode)
	}
	return nil
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

func (s *Server) rollbackGitHubRepositoryWebhooks(
	r *http.Request,
	token string,
	hooks []githubCreatedHook,
) {
	if len(hooks) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), defaultGitHubHTTPTimeout)
	defer cancel()
	if err := s.deleteGitHubRepositoryWebhooks(ctx, token, hooks); err != nil {
		log.Printf(
			"github webhook rollback failed correlation_id=%q hook_count=%d error_type=%T",
			correlationIDFromContext(r.Context()),
			len(hooks),
			err,
		)
	}
}

func (s *Server) deleteGitHubRepositoryWebhooks(
	ctx context.Context,
	token string,
	hooks []githubCreatedHook,
) error {
	var rollbackErrors []error
	for i := len(hooks) - 1; i >= 0; i-- {
		hook := hooks[i]
		var err error
		for attempt := 0; attempt < githubWebhookRollbackAttempts; attempt++ {
			err = s.deleteGitHubRepositoryWebhook(ctx, token, hook)
			if err == nil {
				break
			}
			if attempt+1 < githubWebhookRollbackAttempts {
				timer := time.NewTimer(time.Duration(attempt+1) * githubWebhookRollbackBackoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					err = ctx.Err()
					attempt = githubWebhookRollbackAttempts
				case <-timer.C:
				}
			}
		}
		if err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
}

func (s *Server) deleteGitHubRepositoryWebhook(
	ctx context.Context,
	token string,
	hook githubCreatedHook,
) error {
	endpoint := strings.TrimRight(s.githubOAuth.APIURL, "/") + "/repos/" +
		url.PathEscape(hook.Repository.Owner) + "/" + url.PathEscape(hook.Repository.Name) +
		"/hooks/" + strconv.FormatInt(hook.ID, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	setGitHubAPIHeaders(req, token)
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return &githubHookError{Repository: hook.Repository.FullName, StatusCode: resp.StatusCode}
	}
	return nil
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
