package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

func (s *Server) requireCookieCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || hasBearerAuth(r) || (!s.hasSessionCookie(r) && !s.hasAccessAssertion(r)) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(csrfHeaderName) != "1" || !s.sameOriginBrowserRequest(r) {
			writeError(w, http.StatusForbidden, errors.New("cross-site session requests are not allowed"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hasAccessAssertion(r *http.Request) bool {
	return s.access != nil && r.Header.Get(accessAssertionHeader) != ""
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func hasBearerAuth(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (s *Server) hasSessionCookie(r *http.Request) bool {
	cookies := r.CookiesNamed(s.cookies.Session)
	if len(cookies) > 1 {
		return true
	}
	return len(cookies) == 1 && cookies[0].Value != ""
}

func requestCookie(r *http.Request, name string) (*http.Cookie, error) {
	cookies := r.CookiesNamed(name)
	switch len(cookies) {
	case 0:
		return nil, http.ErrNoCookie
	case 1:
		return cookies[0], nil
	default:
		return nil, errAmbiguousCookie
	}
}
