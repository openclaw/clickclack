package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/authpolicy"
)

var corsMethods = map[string]struct{}{
	http.MethodGet: {}, http.MethodHead: {}, http.MethodPost: {}, http.MethodPut: {},
	http.MethodPatch: {}, http.MethodDelete: {}, http.MethodOptions: {},
}

var corsHeaders = map[string]struct{}{
	"authorization": {}, "content-type": {}, "x-clickclack-csrf": {}, "x-request-id": {},
}

func configuredCookieSameSite(frontendURL, publicAPIURL string) http.SameSite {
	frontend, frontendErr := url.Parse(strings.TrimSpace(frontendURL))
	api, apiErr := url.Parse(strings.TrimSpace(publicAPIURL))
	if frontendErr == nil && apiErr == nil && frontend.Hostname() != "" && api.Hostname() != "" && !strings.EqualFold(frontend.Hostname(), api.Hostname()) {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.sameOrigin(r, origin) {
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				writeError(w, http.StatusForbidden, errors.New("origin is not allowed"))
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		canonical, ok := canonicalOriginString(origin)
		if !ok {
			writeError(w, http.StatusForbidden, errors.New("origin is not allowed"))
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", canonical)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")
		method := strings.ToUpper(strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")))
		if _, ok := corsMethods[method]; !ok {
			writeError(w, http.StatusForbidden, errors.New("CORS method is not allowed"))
			return
		}
		requestedHeaders, ok := allowedCORSHeaders(r.Header.Get("Access-Control-Request-Headers"))
		if !ok {
			writeError(w, http.StatusForbidden, errors.New("CORS header is not allowed"))
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", method)
		if requestedHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
		}
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
	})
}

func allowedCORSHeaders(value string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return "", true
	}
	var allowed []string
	for _, header := range strings.Split(value, ",") {
		header = strings.ToLower(strings.TrimSpace(header))
		if _, ok := corsHeaders[header]; !ok {
			return "", false
		}
		allowed = append(allowed, header)
	}
	sort.Strings(allowed)
	return strings.Join(allowed, ", "), true
}

func canonicalOriginString(origin string) (string, bool) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return canonicalOrigin(parsed)
}

func (s *Server) apiBaseURL(r *http.Request) string {
	if s.publicAPIURL != "" {
		return s.publicAPIURL
	}
	if !isLocalDevRequest(r) {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	base, err := authpolicy.CanonicalPublicAPIURL(scheme + "://" + r.Host)
	if err != nil {
		return ""
	}
	return base
}
