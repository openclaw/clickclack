package httpapi

import (
	"database/sql"
	"errors"
	"log"
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

func (s *Server) channelDeletionPreview(w http.ResponseWriter, r *http.Request) {
	act, ok := s.channelDeletionActor(w, r)
	if !ok {
		return
	}
	preview, err := s.store.PreviewChannelDeletion(r.Context(), chi.URLParam(r, "channel_id"), act.user.ID)
	if err != nil {
		writeChannelDeletionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	act, ok := s.channelDeletionActor(w, r)
	if !ok {
		return
	}
	deletion, err := s.store.DeleteChannel(r.Context(), chi.URLParam(r, "channel_id"), act.user.ID)
	if err != nil {
		writeChannelDeletionError(w, err)
		return
	}
	s.publishEvent(r.Context(), deletion.Event)
	s.recordAudit(r.Context(), deletion.Channel.WorkspaceID, act.user.ID, "channel.deleted", "channel", deletion.Channel.ID, map[string]any{
		"name":           deletion.Channel.Name,
		"messages":       deletion.Counts.Messages,
		"thread_replies": deletion.Counts.ThreadReplies,
		"files":          deletion.Counts.Files,
	})
	if err := s.cleanupUploadObjects(r.Context(), deletion.Cleanups); err != nil {
		log.Printf("channel %s deleted with pending upload cleanup retry: %v", deletion.Channel.ID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// channelDeletionActor admits human sessions only; owner authorization happens
// in the store transaction.
func (s *Server) channelDeletionActor(w http.ResponseWriter, r *http.Request) (actor, bool) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return actor{}, false
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot delete channels"))
		return actor{}, false
	}
	if err := act.requireScope("channels:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return actor{}, false
	}
	return act, true
}

func writeChannelDeletionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, errors.New("channel not found"))
	case errors.Is(err, store.ErrLastChannel), errors.Is(err, store.ErrProvisionedChannel):
		writeError(w, http.StatusConflict, err)
	default:
		writeStoreError(w, err)
	}
}
