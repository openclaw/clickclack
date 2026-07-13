package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"golang.org/x/oauth2"
)

type GitHubOAuthConfig struct {
	ClientID      string
	ClientSecret  string
	PublicURL     string
	AuthURL       string
	TokenURL      string
	UserURL       string
	EmailsURL     string
	MembershipURL string
	AllowedOrg    string
	ModeratorOrg  string
	HTTPClient    *http.Client
}

var errGitHubOrgDenied = errors.New("github account is not a member of the allowed organization")

const defaultGitHubHTTPTimeout = 30 * time.Second

const (
	desktopOAuthLegacyCallbackURL = "clickclack://auth/callback"
	desktopOAuthCallbackURL       = "chat.clickclack.desktop:/auth/callback"
	oauthTransactionTTL           = 10 * time.Minute
	oauthBrowserBindingTTL        = 30 * time.Minute
	desktopOAuthGrantTTL          = 5 * time.Minute
	oauthSecretBytes              = 32
	oauthEncodedSecretLength      = 43
)

const (
	githubOAuthEventBrowserStart            = "browser_start"
	githubOAuthEventDesktopStart            = "desktop_start"
	githubOAuthEventStartRejected           = "start_rejected"
	githubOAuthEventCapacityRejected        = "capacity_rejected"
	githubOAuthEventStateRejected           = "state_rejected"
	githubOAuthEventProviderFailed          = "provider_failed"
	githubOAuthEventOrgDenied               = "org_denied"
	githubOAuthEventBrowserSucceeded        = "browser_succeeded"
	githubOAuthEventDesktopGrantCreated     = "desktop_grant_created"
	githubOAuthEventDesktopProtocolRejected = "desktop_protocol_rejected"
	githubOAuthEventDesktopUpgradeRequired  = "desktop_upgrade_required"
	githubOAuthEventDesktopConsumeRejected  = "desktop_consume_rejected"
	githubOAuthEventDesktopConsumeSucceeded = "desktop_consume_succeeded"
)

func (c GitHubOAuthConfig) withDefaults() GitHubOAuthConfig {
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.ClientSecret = strings.TrimSpace(c.ClientSecret)
	c.PublicURL = strings.TrimSpace(c.PublicURL)
	c.AllowedOrg = strings.TrimSpace(c.AllowedOrg)
	c.ModeratorOrg = strings.TrimSpace(c.ModeratorOrg)
	if c.AuthURL == "" {
		c.AuthURL = "https://github.com/login/oauth/authorize"
	}
	if c.TokenURL == "" {
		c.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if c.UserURL == "" {
		c.UserURL = "https://api.github.com/user"
	}
	if c.EmailsURL == "" {
		c.EmailsURL = "https://api.github.com/user/emails"
	}
	if c.MembershipURL == "" {
		c.MembershipURL = "https://api.github.com/user/memberships/orgs/"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: defaultGitHubHTTPTimeout}
	}
	return c
}

func (s *Server) githubStart(w http.ResponseWriter, r *http.Request) {
	s.recordGitHubOAuthEvent(githubOAuthEventBrowserStart)
	s.startGitHubOAuth(w, r, "", 0)
}

func (s *Server) githubDesktopStart(w http.ResponseWriter, r *http.Request) {
	s.recordGitHubOAuthEvent(githubOAuthEventDesktopStart)
	challenge := strings.TrimSpace(r.URL.Query().Get("code_challenge"))
	if !validDesktopCode(challenge, 43, 43) {
		s.recordGitHubOAuthEvent(githubOAuthEventStartRejected)
		writeError(w, http.StatusBadRequest, errors.New("valid desktop oauth code challenge is required"))
		return
	}
	protocol, err := desktopProtocolVersion(r.URL.Query().Get("desktop_protocol"))
	if err != nil {
		s.recordGitHubOAuthEvent(githubOAuthEventDesktopProtocolRejected)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.cookies.Namespaced && protocol < 2 {
		s.recordGitHubOAuthEvent(githubOAuthEventDesktopUpgradeRequired)
		writeDesktopUpgradeRequired(w)
		return
	}
	s.startGitHubOAuth(w, r, challenge, protocol)
}

func (s *Server) startGitHubOAuth(w http.ResponseWriter, r *http.Request, desktopChallenge string, desktopProtocol int64) {
	if s.githubOAuth.ClientID == "" || s.githubOAuth.ClientSecret == "" {
		s.recordGitHubOAuthEvent(githubOAuthEventStartRejected)
		writeError(w, http.StatusNotImplemented, errors.New("github oauth is not configured"))
		return
	}
	redirectURL, err := s.githubRedirectURL(r)
	if err != nil {
		s.recordGitHubOAuthEvent(githubOAuthEventStartRejected)
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	browserBinding, err := s.oauthBrowserBinding(w, r)
	if err != nil {
		s.recordGitHubOAuthEvent(githubOAuthEventStartRejected)
		if errors.Is(err, errAmbiguousCookie) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	state, err := randomOAuthSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	pkceVerifier, err := randomOAuthSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	mode := store.OAuthModeBrowser
	if desktopChallenge != "" {
		mode = store.OAuthModeDesktop
	}
	now := time.Now().UTC()
	if err := s.store.CreateOAuthTransaction(r.Context(), store.OAuthTransaction{
		StateHash:          secretHash(state),
		BrowserBindingHash: secretHash(browserBinding),
		Mode:               mode,
		PKCEVerifier:       pkceVerifier,
		DesktopChallenge:   desktopChallenge,
		DesktopProtocol:    desktopProtocol,
		CreatedAt:          now,
		ExpiresAt:          now.Add(oauthTransactionTTL),
	}); err != nil {
		if errors.Is(err, store.ErrOAuthCapacityExceeded) {
			s.recordGitHubOAuthEvent(githubOAuthEventCapacityRejected)
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		s.recordGitHubOAuthEvent(githubOAuthEventStartRejected)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, s.oauth2Config(redirectURL).AuthCodeURL(state, oauth2.S256ChallengeOption(pkceVerifier)), http.StatusFound)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if !validDesktopCode(state, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		s.recordGitHubOAuthEvent(githubOAuthEventStateRejected)
		writeError(w, http.StatusBadRequest, errors.New("invalid github oauth state"))
		return
	}
	bindingCookie, err := requestCookie(r, s.cookies.OAuthBinding)
	if err != nil || !validDesktopCode(bindingCookie.Value, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		s.recordGitHubOAuthEvent(githubOAuthEventStateRejected)
		writeError(w, http.StatusBadRequest, errors.New("invalid github oauth state"))
		return
	}
	transaction, err := s.store.ConsumeOAuthTransaction(r.Context(), secretHash(state), secretHash(bindingCookie.Value), time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrOAuthTransactionInvalid) {
			s.recordGitHubOAuthEvent(githubOAuthEventStateRejected)
			writeError(w, http.StatusBadRequest, errors.New("invalid github oauth state"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, errors.New("github oauth code is required"))
		return
	}
	redirectURL, err := s.githubRedirectURL(r)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	token, err := s.exchangeGitHubCode(r.Context(), code, transaction.PKCEVerifier, redirectURL)
	if err != nil {
		s.recordGitHubOAuthEvent(githubOAuthEventProviderFailed)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	profile, err := s.fetchGitHubProfile(r.Context(), token)
	if err != nil {
		s.recordGitHubOAuthEvent(githubOAuthEventProviderFailed)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := s.ensureGitHubAllowedOrgMembership(r.Context(), token); err != nil {
		if errors.Is(err, errGitHubOrgDenied) {
			s.recordGitHubOAuthEvent(githubOAuthEventOrgDenied)
			writeError(w, http.StatusForbidden, err)
			return
		}
		s.recordGitHubOAuthEvent(githubOAuthEventProviderFailed)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	user, err := s.store.UpsertIdentityUser(r.Context(), store.UpsertIdentityUserInput{
		Provider:        "github",
		ProviderSubject: strconv.FormatInt(profile.ID, 10),
		Email:           profile.Email,
		DisplayName:     firstNonEmpty(profile.Name, profile.Login, profile.Email),
		AvatarURL:       profile.AvatarURL,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := s.ensureGitHubWorkspace(r.Context(), token, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if transaction.Mode == store.OAuthModeBrowser {
		session, err := s.store.CreateSession(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.setSessionCookie(w, r, session)
		s.recordGitHubOAuthEvent(githubOAuthEventBrowserSucceeded)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if transaction.Mode != store.OAuthModeDesktop || !validDesktopCode(transaction.DesktopChallenge, 43, 43) {
		writeError(w, http.StatusBadRequest, errors.New("invalid desktop oauth transaction"))
		return
	}
	grantCode, err := newDesktopOAuthGrantCode(transaction.DesktopProtocol)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().UTC()
	if err := s.store.CreateDesktopOAuthGrant(r.Context(), store.DesktopOAuthGrant{
		GrantHash:        secretHash(grantCode),
		UserID:           user.ID,
		DesktopChallenge: transaction.DesktopChallenge,
		CreatedAt:        now,
		ExpiresAt:        now.Add(desktopOAuthGrantTTL),
	}); err != nil {
		if errors.Is(err, store.ErrOAuthCapacityExceeded) {
			s.recordGitHubOAuthEvent(githubOAuthEventCapacityRejected)
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.recordGitHubOAuthEvent(githubOAuthEventDesktopGrantCreated)
	http.Redirect(w, r, desktopOAuthCallback(transaction.DesktopProtocol, grantCode), http.StatusFound)
}

func desktopProtocolVersion(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	protocol, err := strconv.ParseInt(value, 10, 64)
	if err != nil || protocol < 1 || protocol > 2 {
		return 0, errors.New("unsupported desktop oauth protocol")
	}
	return protocol, nil
}

func writeDesktopUpgradeRequired(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusUpgradeRequired)
	_, _ = w.Write([]byte("<!doctype html><meta charset=utf-8><title>ClickClack update required</title><h1>Update ClickClack to sign in</h1><p>This server requires a newer ClickClack desktop app.</p>"))
}

func (s *Server) githubDesktopConsume(w http.ResponseWriter, r *http.Request) {
	if !s.requireSameOriginJSON(w, r) {
		s.recordGitHubOAuthEvent(githubOAuthEventDesktopConsumeRejected)
		return
	}
	var body struct {
		Code     string `json:"code"`
		Verifier string `json:"code_verifier"`
	}
	if err := readJSON(w, r, &body); err != nil {
		s.recordGitHubOAuthEvent(githubOAuthEventDesktopConsumeRejected)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !validOAuthGrantCode(body.Code) || !validDesktopCode(body.Verifier, 43, 128) {
		s.recordGitHubOAuthEvent(githubOAuthEventDesktopConsumeRejected)
		writeError(w, http.StatusBadRequest, errors.New("invalid desktop oauth grant"))
		return
	}
	session, err := s.store.ConsumeDesktopOAuthGrant(r.Context(), secretHash(body.Code), desktopCodeChallenge(body.Verifier), time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrDesktopOAuthGrantInvalid) {
			s.recordGitHubOAuthEvent(githubOAuthEventDesktopConsumeRejected)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.setSessionCookie(w, r, session)
	s.recordGitHubOAuthEvent(githubOAuthEventDesktopConsumeSucceeded)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func desktopCodeChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validDesktopCode(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) ensureGitHubWorkspace(ctx context.Context, token, userID string) (store.Workspace, error) {
	moderatorOrg := strings.TrimSpace(s.githubOAuth.ModeratorOrg)
	if strings.TrimSpace(s.githubOAuth.AllowedOrg) != "" && moderatorOrg == "" {
		return s.store.EnsureDefaultWorkspaceMember(ctx, userID)
	}
	role := store.WorkspaceRoleMember
	if moderatorOrg != "" {
		role = store.WorkspaceRoleGuest
		if strings.TrimSpace(s.githubOAuth.AllowedOrg) != "" {
			role = store.WorkspaceRoleMember
		}
		ok, err := s.githubOrgMembership(ctx, token, moderatorOrg)
		if err != nil {
			return store.Workspace{}, err
		}
		if ok {
			role = store.WorkspaceRoleModerator
		}
	}
	return s.store.EnsureDefaultGuestWorkspaceMember(ctx, userID, role)
}

func (s *Server) exchangeGitHubCode(ctx context.Context, code, verifier, redirectURL string) (string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.githubOAuth.HTTPClient)
	token, err := s.oauth2Config(redirectURL).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", errors.New("github token exchange failed")
	}
	if token.AccessToken == "" {
		return "", errors.New("github access token missing")
	}
	return token.AccessToken, nil
}

func (s *Server) oauth2Config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.githubOAuth.ClientID,
		ClientSecret: s.githubOAuth.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   s.githubOAuth.AuthURL,
			TokenURL:  s.githubOAuth.TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RedirectURL: redirectURL,
		Scopes:      strings.Fields(s.githubScope()),
	}
}

type githubProfile struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (s *Server) fetchGitHubProfile(ctx context.Context, token string) (githubProfile, error) {
	var profile githubProfile
	if err := s.githubGetJSON(ctx, s.githubOAuth.UserURL, token, &profile); err != nil {
		return githubProfile{}, err
	}
	if profile.ID == 0 {
		return githubProfile{}, errors.New("github profile id missing")
	}
	if profile.Email == "" {
		var emails []struct {
			Email   string `json:"email"`
			Primary bool   `json:"primary"`
		}
		if err := s.githubGetJSON(ctx, s.githubOAuth.EmailsURL, token, &emails); err != nil {
			return githubProfile{}, err
		}
		for _, item := range emails {
			if item.Primary {
				profile.Email = item.Email
				break
			}
		}
	}
	return profile, nil
}

func (s *Server) githubGetJSON(ctx context.Context, endpoint, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("github api request failed")
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Server) ensureGitHubAllowedOrgMembership(ctx context.Context, token string) error {
	org := strings.TrimSpace(s.githubOAuth.AllowedOrg)
	if org == "" {
		return nil
	}
	ok, err := s.githubOrgMembership(ctx, token, org)
	if err != nil {
		return err
	}
	if !ok {
		return errGitHubOrgDenied
	}
	return nil
}

func (s *Server) githubOrgMembership(ctx context.Context, token, org string) (bool, error) {
	org = strings.TrimSpace(org)
	if org == "" {
		return false, nil
	}
	endpoint := strings.TrimRight(s.githubOAuth.MembershipURL, "/") + "/" + url.PathEscape(org)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return false, nil
	}
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("github organization membership check failed: %s", resp.Status)
	}
	var membership struct {
		State        string `json:"state"`
		Organization struct {
			Login string `json:"login"`
		} `json:"organization"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		return false, err
	}
	if !strings.EqualFold(membership.State, "active") || !strings.EqualFold(membership.Organization.Login, org) {
		return false, nil
	}
	return true, nil
}

func (s *Server) githubScope() string {
	scope := "read:user user:email"
	if strings.TrimSpace(s.githubOAuth.AllowedOrg) != "" || strings.TrimSpace(s.githubOAuth.ModeratorOrg) != "" {
		scope += " read:org"
	}
	return scope
}

func (s *Server) githubRedirectURL(r *http.Request) (string, error) {
	base := strings.TrimRight(s.githubOAuth.PublicURL, "/")
	if base == "" {
		if s.disableDevAuth || !isLocalHostPort(r.Host) || !isLocalHostPort(r.RemoteAddr) {
			return "", errors.New("github oauth requires a configured public URL")
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/api/auth/github/callback", nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, session store.Session) {
	expires, _ := time.Parse(time.RFC3339Nano, session.ExpiresAt)
	http.SetCookie(w, &http.Cookie{Name: s.cookies.Session, Value: session.Token, Path: "/", Expires: expires, HttpOnly: true, Secure: s.secureCookies(r), SameSite: http.SameSiteLaxMode})
}

func (s *Server) secureCookies(r *http.Request) bool {
	if publicURL, err := url.Parse(strings.TrimSpace(s.githubOAuth.PublicURL)); err == nil {
		if publicURL.Scheme == "https" {
			return true
		}
	}
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	if publicURL, err := url.Parse(strings.TrimSpace(s.githubOAuth.PublicURL)); err == nil {
		if !s.disableDevAuth && publicURL.Scheme == "http" && isLocalHostPort(publicURL.Host) {
			return false
		}
	}
	return !(!s.disableDevAuth && isLocalHostPort(r.RemoteAddr) && isLocalHostPort(r.Host))
}

func (s *Server) oauthBrowserBinding(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := requestCookie(r, s.cookies.OAuthBinding); err == nil && validDesktopCode(cookie.Value, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		return cookie.Value, nil
	} else if errors.Is(err, errAmbiguousCookie) {
		return "", err
	}
	binding, err := randomOAuthSecret()
	if err != nil {
		return "", err
	}
	maxAge := int(oauthBrowserBindingTTL / time.Second)
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookies.OAuthBinding,
		Value:    binding,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  time.Now().UTC().Add(oauthBrowserBindingTTL),
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
	return binding, nil
}

func randomOAuthSecret() (string, error) {
	data := make([]byte, oauthSecretBytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func randomLegacyOAuthGrant() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func newDesktopOAuthGrantCode(protocol int64) (string, error) {
	if protocol == 1 {
		return randomLegacyOAuthGrant()
	}
	return randomOAuthSecret()
}

func desktopOAuthCallback(protocol int64, grantCode string) string {
	callbackURL := desktopOAuthLegacyCallbackURL
	if protocol >= 2 {
		callbackURL = desktopOAuthCallbackURL
	}
	callback, _ := url.Parse(callbackURL)
	query := callback.Query()
	query.Set("code", grantCode)
	callback.RawQuery = query.Encode()
	return callback.String()
}

func validOAuthGrantCode(value string) bool {
	if validDesktopCode(value, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		return true
	}
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func secretHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
