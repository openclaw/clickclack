package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireScope("channels:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	channels, err := s.store.ListChannels(r.Context(), workspaceID, act.user.ID)
	writeResult(w, map[string]any{"channels": channels}, err)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("channels:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(chi.URLParam(r, "workspace_id")); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Name            string `json:"name"`
		DisplayTitle    string `json:"display_title"`
		Kind            string `json:"kind"`
		ExternalManaged bool   `json:"external_managed"`
		ExternalRef     string `json:"external_ref"`
		ExternalURL     string `json:"external_url"`
		SidebarSection  string `json:"sidebar_section"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	channel, event, err := s.store.CreateChannel(r.Context(), store.CreateChannelInput{
		WorkspaceID:     chi.URLParam(r, "workspace_id"),
		Name:            body.Name,
		DisplayTitle:    body.DisplayTitle,
		Kind:            body.Kind,
		UserID:          act.user.ID,
		ExternalManaged: body.ExternalManaged,
		ExternalRef:     body.ExternalRef,
		ExternalURL:     body.ExternalURL,
		SidebarSection:  body.SidebarSection,
	})
	if err == nil {
		s.publishEvent(r.Context(), event)
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{"channel": channel, "event": event}, err)
}

func (s *Server) listTopics(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("channels:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	topics, err := s.store.ListTopics(r.Context(), workspaceID, act.user.ID)
	writeResult(w, map[string]any{"topics": topics}, err)
}

func (s *Server) createTopic(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("channels:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	topic, err := s.store.CreateTopic(r.Context(), store.CreateTopicInput{WorkspaceID: workspaceID, ChannelID: body.ChannelID, Name: body.Name, CreatedBy: act.user.ID})
	writeResultStatus(w, http.StatusCreated, map[string]any{"topic": topic}, err)
}
