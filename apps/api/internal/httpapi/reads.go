package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) markChannelRead(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Seq int64 `json:"seq"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.requireBotChannelWorkspace(w, r, act, chi.URLParam(r, "channel_id")) {
		return
	}
	receipt, event, err := s.store.MarkChannelRead(r.Context(), chi.URLParam(r, "channel_id"), act.user.ID, body.Seq)
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	writeResult(w, map[string]any{"receipt": receipt}, err)
}

func (s *Server) markDirectRead(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Seq int64 `json:"seq"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("dms:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.requireBotDirectWorkspace(w, r, act, chi.URLParam(r, "conversation_id")) {
		return
	}
	receipt, event, err := s.store.MarkDirectRead(r.Context(), chi.URLParam(r, "conversation_id"), act.user.ID, body.Seq)
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	writeResult(w, map[string]any{"receipt": receipt}, err)
}
