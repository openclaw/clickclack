package httpapi

import (
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	if fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); fetchSite != "" && fetchSite != "same-origin" && fetchSite != "none" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	return s.sameOrigin(r, origin)
}

func (s *Server) sameOrigin(r *http.Request, origin string) bool {
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Host == "" {
		return false
	}
	if parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https" {
		return false
	}
	if publicURL, err := url.Parse(strings.TrimSpace(s.githubOAuth.PublicURL)); err == nil && publicURL.Scheme != "" && publicURL.Host != "" {
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
