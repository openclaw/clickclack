package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
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
	desktopOAuthCallbackURL = "clickclack://auth/callback"
	desktopOAuthStateTTL    = 10 * time.Minute
	desktopOAuthGrantTTL    = 5 * time.Minute
	desktopOAuthMaxGrants   = 4096
)

type desktopOAuthGrant struct {
	challenge string
	expiresAt time.Time
	session   store.Session
}

type desktopOAuthBroker struct {
	mu     sync.Mutex
	grants map[string]desktopOAuthGrant
}

func newDesktopOAuthBroker() *desktopOAuthBroker {
	return &desktopOAuthBroker{
		grants: make(map[string]desktopOAuthGrant),
	}
}

func (c GitHubOAuthConfig) withDefaults() GitHubOAuthConfig {
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
	s.startGitHubOAuth(w, r, "")
}

func (s *Server) githubDesktopStart(w http.ResponseWriter, r *http.Request) {
	challenge := strings.TrimSpace(r.URL.Query().Get("code_challenge"))
	if !validDesktopCode(challenge, 43, 43) {
		writeError(w, http.StatusBadRequest, errors.New("valid desktop oauth code challenge is required"))
		return
	}
	s.startGitHubOAuth(w, r, challenge)
}

func (s *Server) startGitHubOAuth(w http.ResponseWriter, r *http.Request, desktopChallenge string) {
	if s.githubOAuth.ClientID == "" || s.githubOAuth.ClientSecret == "" {
		writeError(w, http.StatusNotImplemented, errors.New("github oauth is not configured"))
		return
	}
	state, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if desktopChallenge != "" {
		maxAge := int(desktopOAuthStateTTL / time.Second)
		http.SetCookie(w, &http.Cookie{Name: "cc_github_desktop_state", Value: state, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: s.secureCookies(r), SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: "cc_github_desktop_challenge", Value: desktopChallenge, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: s.secureCookies(r), SameSite: http.SameSiteLaxMode})
	} else {
		s.clearGitHubDesktopStateCookie(w, r)
		s.clearGitHubDesktopChallengeCookie(w, r)
	}
	http.SetCookie(w, &http.Cookie{Name: "cc_github_state", Value: state, Path: "/", MaxAge: 600, HttpOnly: true, Secure: s.secureCookies(r), SameSite: http.SameSiteLaxMode})
	values := url.Values{
		"client_id":    {s.githubOAuth.ClientID},
		"redirect_uri": {s.githubRedirectURL(r)},
		"scope":        {s.githubScope()},
		"state":        {state},
	}
	http.Redirect(w, r, s.githubOAuth.AuthURL+"?"+values.Encode(), http.StatusFound)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	state, err := r.Cookie("cc_github_state")
	if err != nil || state.Value == "" || state.Value != r.URL.Query().Get("state") {
		writeError(w, http.StatusBadRequest, errors.New("invalid github oauth state"))
		return
	}
	s.clearGitHubStateCookie(w, r)
	desktopStateCookie, desktopCookieErr := r.Cookie("cc_github_desktop_state")
	isDesktop := desktopCookieErr == nil && desktopStateCookie.Value == state.Value
	s.clearGitHubDesktopStateCookie(w, r)
	desktopChallengeCookie, desktopChallengeErr := r.Cookie("cc_github_desktop_challenge")
	s.clearGitHubDesktopChallengeCookie(w, r)
	var desktopChallenge string
	if isDesktop {
		if desktopChallengeErr != nil || !validDesktopCode(desktopChallengeCookie.Value, 43, 43) {
			writeError(w, http.StatusBadRequest, errors.New("desktop oauth request expired"))
			return
		}
		desktopChallenge = desktopChallengeCookie.Value
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, errors.New("github oauth code is required"))
		return
	}
	token, err := s.exchangeGitHubCode(r.Context(), r, code)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	profile, err := s.fetchGitHubProfile(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := s.ensureGitHubAllowedOrgMembership(r.Context(), token); err != nil {
		if errors.Is(err, errGitHubOrgDenied) {
			writeError(w, http.StatusForbidden, err)
			return
		}
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
	session, err := s.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !isDesktop {
		s.setSessionCookie(w, r, session)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	grantCode, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !s.desktopOAuth.putGrant(grantCode, desktopChallenge, session, time.Now().Add(desktopOAuthGrantTTL)) {
		writeError(w, http.StatusServiceUnavailable, errors.New("too many pending desktop oauth grants"))
		return
	}
	callback, _ := url.Parse(desktopOAuthCallbackURL)
	query := callback.Query()
	query.Set("code", grantCode)
	callback.RawQuery = query.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound)
}

func (s *Server) githubDesktopConsume(w http.ResponseWriter, r *http.Request) {
	if !s.requireSameOriginJSON(w, r) {
		return
	}
	var body struct {
		Code     string `json:"code"`
		Verifier string `json:"code_verifier"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !validDesktopCode(body.Code, 32, 32) || !validDesktopCode(body.Verifier, 43, 128) {
		writeError(w, http.StatusBadRequest, errors.New("invalid desktop oauth grant"))
		return
	}
	session, ok := s.desktopOAuth.consumeGrant(body.Code, desktopCodeChallenge(body.Verifier), time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid or expired desktop oauth grant"))
		return
	}
	s.setSessionCookie(w, r, session)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) clearGitHubStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "cc_github_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearGitHubDesktopStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "cc_github_desktop_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearGitHubDesktopChallengeCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "cc_github_desktop_challenge",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (b *desktopOAuthBroker) putGrant(code, challenge string, session store.Session, expiresAt time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(time.Now())
	if len(b.grants) >= desktopOAuthMaxGrants {
		return false
	}
	b.grants[code] = desktopOAuthGrant{challenge: challenge, expiresAt: expiresAt, session: session}
	return true
}

func (b *desktopOAuthBroker) consumeGrant(code, challenge string, now time.Time) (store.Session, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	grant, ok := b.grants[code]
	if !ok || subtle.ConstantTimeCompare([]byte(grant.challenge), []byte(challenge)) != 1 {
		return store.Session{}, false
	}
	delete(b.grants, code)
	return grant.session, true
}

func (b *desktopOAuthBroker) pruneLocked(now time.Time) {
	for key, grant := range b.grants {
		if !now.Before(grant.expiresAt) {
			delete(b.grants, key)
		}
	}
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

func (s *Server) exchangeGitHubCode(ctx context.Context, r *http.Request, code string) (string, error) {
	body := url.Values{
		"client_id":     {s.githubOAuth.ClientID},
		"client_secret": {s.githubOAuth.ClientSecret},
		"code":          {code},
		"redirect_uri":  {s.githubRedirectURL(r)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.githubOAuth.TokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.githubOAuth.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", errors.New("github token exchange failed")
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	if out.AccessToken == "" {
		return "", errors.New("github access token missing")
	}
	return out.AccessToken, nil
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

func (s *Server) githubRedirectURL(r *http.Request) string {
	base := strings.TrimRight(s.githubOAuth.PublicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/api/auth/github/callback"
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

func randomToken() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
