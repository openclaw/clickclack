package httpapi

import (
	"context"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/passwordauth"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type magicLinkResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	UsedAt      *string `json:"used_at,omitempty"`
}

func (s *Server) requestMagicLink(w http.ResponseWriter, r *http.Request) {
	if s.disableDevAuth {
		writeError(w, http.StatusNotImplemented, errors.New("magic-link delivery is not configured"))
		return
	}
	if !isLocalDevRequest(r) {
		writeError(w, http.StatusForbidden, errors.New("magic-link token minting is only available from loopback clients"))
		return
	}
	var body struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	link, err := s.store.CreateMagicLink(r.Context(), body.Email, body.DisplayName)
	response := map[string]any{"magic_link": magicLinkResponse{
		ID:          link.ID,
		Email:       link.Email,
		DisplayName: link.DisplayName,
		CreatedAt:   link.CreatedAt,
		ExpiresAt:   link.ExpiresAt,
		UsedAt:      link.UsedAt,
	}}
	response["token"] = link.Token
	writeResultStatus(w, http.StatusCreated, response, err)
}

func (s *Server) consumeMagicLink(w http.ResponseWriter, r *http.Request) {
	if !s.requireSameOriginJSON(w, r) {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, session, err := s.store.ConsumeMagicLink(r.Context(), body.Token)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.setSessionCookie(w, r, session)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "session": session, "token": session.Token})
}

// errPasswordLoginRejected is deliberately the same message for a wrong
// password, an unknown identifier, and an account with no password on file, so
// the response never discloses which accounts can sign in this way.
var errPasswordLoginRejected = errors.New("invalid identifier or password")

func (s *Server) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !s.passwordAuthEnabled {
		writeError(w, http.StatusNotImplemented, errors.New("password login is not configured"))
		return
	}
	if !s.requireSameOriginJSON(w, r) {
		return
	}
	var body struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	identifier := strings.ToLower(strings.TrimSpace(body.Identifier))
	if identifier == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, errors.New("identifier and password are required"))
		return
	}
	// Every attempt costs the caller's address budget, but only failures cost
	// the account's, so a correct password never contributes to the account
	// owner's own lockout.
	if !s.passwordIPLimiter.allow(clientIPKey(r)) || s.passwordIDLimiter.blocked(identifier) {
		writeError(w, http.StatusTooManyRequests, errors.New("too many sign-in attempts, retry later"))
		return
	}
	user, ok := s.verifyPasswordLogin(r.Context(), identifier, body.Password)
	if !ok {
		s.passwordIDLimiter.record(identifier)
		writeError(w, http.StatusUnauthorized, errPasswordLoginRejected)
		return
	}
	session, err := s.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.setSessionCookie(w, r, session)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "session": session, "token": session.Token})
}

// verifyPasswordLogin resolves an identifier and checks the secret. Accounts
// that do not exist, are ambiguous, or have no password set still pay for one
// key derivation so that failures cost comparable wall time.
func (s *Server) verifyPasswordLogin(ctx context.Context, identifier, password string) (store.User, bool) {
	login, err := s.store.GetPasswordLogin(ctx, identifier)
	if err != nil || login.PasswordHash == "" {
		passwordauth.VerifyDecoy(password)
		return store.User{}, false
	}
	matched, err := passwordauth.Verify(login.PasswordHash, password)
	if err != nil || !matched {
		return store.User{}, false
	}
	return login.User, true
}

// errPasswordChangeUnenrolled is returned when the caller has no password on
// file. This endpoint deliberately never creates the first one: enabling
// password sign-in for an account stays an administrator action.
var errPasswordChangeUnenrolled = errors.New("this account has no password set; ask an administrator to enable password sign-in first")

// changePassword replaces the caller's own password after checking the current
// one. It is the self-service half of clickclack admin user set-password: an
// operator hands out a temporary password, and the account owner replaces it
// here without the operator ever learning the replacement.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if !s.passwordAuthEnabled {
		writeError(w, http.StatusNotImplemented, errors.New("password login is not configured"))
		return
	}
	if !s.requireSameOriginJSON(w, r) {
		return
	}
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot change passwords"))
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.CurrentPassword == "" || body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, errors.New("current_password and new_password are required"))
		return
	}
	if err := passwordauth.ValidatePassword(body.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// Only wrong guesses spend the budget, so rotating a password several times
	// in a row never locks the account owner out of their own account.
	if s.passwordChangeLimiter.blocked(act.user.ID) {
		writeError(w, http.StatusTooManyRequests, errors.New("too many password change attempts, retry later"))
		return
	}
	stored, err := s.store.GetUserPasswordHash(r.Context(), act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if stored == "" {
		writeError(w, http.StatusConflict, errPasswordChangeUnenrolled)
		return
	}
	matched, err := passwordauth.Verify(stored, body.CurrentPassword)
	if err != nil || !matched {
		s.passwordChangeLimiter.record(act.user.ID)
		writeError(w, http.StatusUnauthorized, errors.New("current password is incorrect"))
		return
	}
	hash, err := passwordauth.Hash(body.NewPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// A password change is how someone locks out a device they no longer
	// control, so every other session for the account ends here. The caller
	// keeps the session it is holding, which leaves the tab it just used signed
	// in. A caller the server cannot place in a session, such as a trusted-proxy
	// assertion, revokes all of them instead.
	//
	// Revocation runs before the new hash is stored, deliberately. These are two
	// statements rather than one transaction, and this order makes the only
	// reachable partial state a recoverable one: a failure here changes nothing,
	// and a failure below leaves the old password working on a signed-in device
	// the owner still holds. The reverse order could report failure after the
	// password had already changed.
	if _, err := s.store.RevokeOtherUserSessions(r.Context(), act.user.ID, requestSessionToken(r, s.cookies.Session)); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.SetUserPassword(r.Context(), act.user.ID, hash); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// requestSessionToken returns the bearer or cookie session token this request
// authenticated with, or an empty string when it used neither.
func requestSessionToken(r *http.Request, cookieName string) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if cookie, err := requestCookie(r, cookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// logout revokes the caller's session and expires the cookie. It succeeds even
// without a valid session so that a stale browser can always return to a
// signed-out state.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.requireSameOriginJSON(w, r) {
		return
	}
	if cookie, err := requestCookie(r, s.cookies.Session); err == nil && cookie.Value != "" {
		if err := s.store.RevokeSession(r.Context(), cookie.Value); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) requireSameOriginJSON(w http.ResponseWriter, r *http.Request) bool {
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, errors.New("content-type must be application/json"))
		return false
	}
	if !s.sameOriginBrowserRequest(r) {
		writeError(w, http.StatusForbidden, errors.New("cross-site login requests are not allowed"))
		return false
	}
	return true
}

func (s *Server) sameOriginBrowserRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return s.sameOrigin(r, origin)
	}
	fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	return fetchSite == "" || fetchSite == "same-origin" || fetchSite == "none"
}

func (s *Server) sameOrigin(r *http.Request, origin string) bool {
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Host == "" {
		return false
	}
	if parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https" {
		return false
	}
	publicBrowserURL := firstNonEmpty(s.frontendURL, s.githubOAuth.PublicURL)
	if publicURL, err := url.Parse(strings.TrimSpace(publicBrowserURL)); err == nil && publicURL.Scheme != "" && publicURL.Host != "" {
		publicOrigin, ok := canonicalOrigin(publicURL)
		if !ok {
			return false
		}
		requestOrigin, ok := canonicalOrigin(parsedOrigin)
		return ok && requestOrigin == publicOrigin
	}
	return originHostMatchesRequest(parsedOrigin, r.Host)
}

func canonicalOrigin(value *url.URL) (string, bool) {
	scheme := strings.ToLower(value.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(value.Hostname())
	if host == "" {
		return "", false
	}
	port := value.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", false
		}
		port = strconv.Itoa(number)
	}
	if port == defaultPort(scheme) {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	}
	return scheme + "://" + host, true
}

func defaultPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func originHostMatchesRequest(origin *url.URL, requestHost string) bool {
	originHost := strings.ToLower(origin.Hostname())
	requestHostname, requestPort := splitHostPort(requestHost)
	if originHost == "" || requestHostname == "" || !strings.EqualFold(originHost, requestHostname) {
		return false
	}
	originPort := origin.Port()
	if originPort == "" {
		originPort = defaultPort(origin.Scheme)
	}
	if requestPort == "" {
		return originPort == defaultPort(origin.Scheme)
	}
	return originPort == requestPort
}

func splitHostPort(value string) (string, string) {
	host := strings.TrimSpace(value)
	if parsedHost, port, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(strings.ToLower(parsedHost), "[]"), port
	}
	return strings.Trim(strings.TrimSuffix(strings.ToLower(host), "."), "[]"), ""
}

func isLocalDevRequest(r *http.Request) bool {
	if !isLocalHostPort(r.RemoteAddr) || !isLocalHostPort(r.Host) {
		return false
	}
	if !localDevBrowserOriginAllowed(r) {
		return false
	}
	if !headerHostsAreLocal(r.Header.Values("X-Forwarded-Host")) {
		return false
	}
	if !headerHostsAreLocal(r.Header.Values("X-Forwarded-For")) || !headerHostsAreLocal(r.Header.Values("X-Real-IP")) {
		return false
	}
	return forwardedHeaderIsLocal(r.Header.Values("Forwarded"))
}

func localDevBrowserOriginAllowed(r *http.Request) bool {
	if fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); fetchSite != "" && fetchSite != "same-origin" && fetchSite != "none" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Host == "" {
		return false
	}
	if parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https" {
		return false
	}
	return originHostMatchesRequest(parsedOrigin, r.Host)
}

func headerHostsAreLocal(values []string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) != "" && !isLocalHostPort(part) {
				return false
			}
		}
	}
	return true
}

func forwardedHeaderIsLocal(values []string) bool {
	for _, value := range values {
		for _, hop := range strings.Split(value, ",") {
			for _, field := range strings.Split(hop, ";") {
				key, raw, ok := strings.Cut(strings.TrimSpace(field), "=")
				if !ok {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "for", "host":
					if !isLocalHostPort(strings.Trim(strings.TrimSpace(raw), `"`)) {
						return false
					}
				}
			}
		}
	}
	return true
}

func isLocalHostPort(value string) bool {
	host := strings.TrimSpace(value)
	if host == "" {
		return false
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
