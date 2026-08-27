package httpapi

import (
	"net/http"
	"strings"
)

// HomeLinkConfig points the workspace rail's home button somewhere other than
// the ClickClack landing page, for deployments that live inside a larger
// product. Empty fields keep the built-in destination and label.
type HomeLinkConfig struct {
	URL   string
	Label string
}

const (
	defaultHomeLinkURL   = "/"
	defaultHomeLinkLabel = "cc"
)

type homeLinkPayload struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

func (s *Server) homeLinkPayload() homeLinkPayload {
	payload := homeLinkPayload{
		URL:   strings.TrimSpace(s.homeLinkConfig.URL),
		Label: strings.TrimSpace(s.homeLinkConfig.Label),
	}
	if payload.URL == "" {
		payload.URL = defaultHomeLinkURL
	}
	if payload.Label == "" {
		payload.Label = defaultHomeLinkLabel
	}
	return payload
}

// homeLink is public: the signed-in shell reads it once at startup and it
// carries no user or workspace data.
func (s *Server) homeLink(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.homeLinkPayload())
}
