package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/webassets"
)

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, errors.New("route not found"))
		return
	}

	dist, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Path != "/" {
		if file, err := dist.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
			_ = file.Close()
			http.FileServer(http.FS(dist)).ServeHTTP(w, r)
			return
		}
	}
	if isMissingBrowserAssetPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	fallback := "index.html"
	if r.URL.Path != "/" {
		if _, err := fs.Stat(dist, "200.html"); err == nil {
			fallback = "200.html"
		}
	}
	index, err := fs.ReadFile(dist, fallback)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(r.URL.Path, "/embed/") {
		// frame-ancestors is deliberately independent of cookie SameSite policy:
		// allowing a cross-site ancestor never loosens cookies, so such embeds can
		// render signed-out. Documented in docs/features/embedding.md.
		ancestors := append([]string{"'self'"}, s.embedFrameAncestors...)
		w.Header().Set("Content-Security-Policy", "frame-ancestors "+strings.Join(ancestors, " "))
	}
	index = s.injectRuntimeConfig(index)
	_, _ = w.Write(index)
}

// enabledAuthMethods tells the frontend which sign-in surfaces to render. It
// is always a non-nil slice so the SPA can distinguish "no method configured"
// from an older server that omitted the field.
func (s *Server) enabledAuthMethods() []string {
	methods := []string{}
	if s.githubOAuth.ClientID != "" && s.githubOAuth.ClientSecret != "" {
		methods = append(methods, "github")
	}
	if s.passwordAuthEnabled {
		methods = append(methods, "password")
	}
	return methods
}

func isMissingBrowserAssetPath(urlPath string) bool {
	if strings.HasPrefix(urlPath, "/_app/") || strings.HasPrefix(urlPath, "/assets/") {
		return true
	}
	switch strings.ToLower(path.Ext(urlPath)) {
	case ".avif", ".css", ".gif", ".ico", ".jpeg", ".jpg", ".js", ".json",
		".map", ".mjs", ".otf", ".png", ".svg", ".ttf", ".wasm", ".webmanifest",
		".webp", ".woff", ".woff2":
		return true
	default:
		return false
	}
}

func (s *Server) injectRuntimeConfig(index []byte) []byte {
	config, err := json.Marshal(struct {
		APIBaseURL      string   `json:"apiBaseUrl"`
		FrontendBaseURL string   `json:"frontendBaseUrl"`
		AuthMethods     []string `json:"authMethods"`
	}{
		APIBaseURL:      s.publicAPIURL,
		FrontendBaseURL: s.frontendURL,
		AuthMethods:     s.enabledAuthMethods(),
	})
	if err != nil {
		return index
	}
	script := append([]byte(`<script>window.__CLICKCLACK_CONFIG__=`), config...)
	script = append(script, []byte(`;</script></head>`)...)
	return bytes.Replace(index, []byte("</head>"), script, 1)
}
