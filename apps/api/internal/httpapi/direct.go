package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listDirectConversations(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if err := act.requireScope("dms:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	items, err := s.store.ListDirectConversations(r.Context(), workspaceID, act.user.ID)
	writeResult(w, map[string]any{"conversations": items}, err)
}

func (s *Server) createDirectConversation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		WorkspaceID string   `json:"workspace_id"`
		MemberIDs   []string `json:"member_ids"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("dms:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(body.WorkspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	dm, err := s.store.CreateDirectConversation(r.Context(), store.CreateDirectConversationInput{WorkspaceID: body.WorkspaceID, UserID: act.user.ID, MemberIDs: body.MemberIDs})
	writeResultStatus(w, http.StatusCreated, map[string]any{"conversation": dm}, err)
}

func (s *Server) getDirectConversation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("dms:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.requireBotDirectWorkspace(w, r, act, chi.URLParam(r, "conversation_id")) {
		return
	}
	dm, err := s.store.GetDirectConversation(r.Context(), chi.URLParam(r, "conversation_id"), act.user.ID)
	writeResult(w, map[string]any{"conversation": dm}, err)
}

func (s *Server) hideDirectConversation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot close direct conversations"))
		return
	}
	if err := act.requireScope("dms:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	err = s.store.HideDirectConversation(r.Context(), chi.URLParam(r, "conversation_id"), act.user.ID)
	writeResult(w, map[string]any{"ok": true}, err)
}

func (s *Server) reopenDirectConversation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot reopen direct conversations"))
		return
	}
	if err := act.requireScope("dms:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	dm, err := s.store.ReopenDirectConversation(r.Context(), chi.URLParam(r, "conversation_id"), act.user.ID)
	writeResult(w, map[string]any{"conversation": dm}, err)
}

func (s *Server) listDirectMessages(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("dms:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	page, err := parseMessagePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.requireBotDirectWorkspace(w, r, act, chi.URLParam(r, "conversation_id")) {
		return
	}
	messages, err := s.store.ListDirectMessages(r.Context(), chi.URLParam(r, "conversation_id"), act.user.ID, page)
	writeMessagePage(w, messages, err)
}

func (s *Server) createDirectMessage(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Body            string `json:"body"`
		QuotedMessageID string `json:"quoted_message_id"`
		Nonce           string `json:"nonce"`
		UploadID        string `json:"upload_id"`
		Kind            string `json:"kind"`
		TurnID          string `json:"turn_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("dms:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	kind, turnID, ok := s.resolveMessageKind(w, act, body.Kind, body.TurnID)
	if !ok {
		return
	}
	if !s.requireBotDirectWorkspace(w, r, act, chi.URLParam(r, "conversation_id")) {
		return
	}
	if !s.requireCreateUpload(w, r, act, body.UploadID, body.Nonce, "", chi.URLParam(r, "conversation_id")) {
		return
	}
	message, event, err := s.store.CreateDirectMessage(r.Context(), store.CreateDirectMessageInput{ConversationID: chi.URLParam(r, "conversation_id"), AuthorID: act.user.ID, Body: body.Body, QuotedMessageID: optionalString(body.QuotedMessageID), Nonce: body.Nonce, UploadID: body.UploadID, Kind: kind, TurnID: turnID})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
		if !store.IsActivityMessageKind(message.Kind) {
			s.notifyMessageCreated(r.Context(), message, event.MentionedUserIDs)
		}
	}
	writeMessageCreateResult(w, message, event, err)
}
