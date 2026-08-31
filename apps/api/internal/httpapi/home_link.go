package httpapi

import "net/http"

// HomeLinkConfig points the workspace rail's home button somewhere other than
// the ClickClack landing page, for deployments that live inside a larger
// product. Values are validated by config.Config.ValidateServe before New;
// empty fields keep the built-in destination and label.
type HomeLinkConfig struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

func (c HomeLinkConfig) withDefaults() HomeLinkConfig {
	if c.URL == "" {
		c.URL = "/"
	}
	if c.Label == "" {
		c.Label = "cc"
	}
	return c
}

// homeLink is public: the signed-in shell reads it once at startup and it
// carries no user or workspace data.
func (s *Server) homeLink(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.homeLinkConfig)
}
