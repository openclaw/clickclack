package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	page, err := parseMessagePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if values, ok := r.URL.Query()["topic_id"]; ok {
		if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: topic_id is required", store.ErrInvalidMessagePage))
			return
		}
		page.TopicID = strings.TrimSpace(values[0])
	}
	if !s.requireBotChannelWorkspace(w, r, act, chi.URLParam(r, "channel_id")) {
		return
	}
	messages, err := s.store.ListMessages(r.Context(), chi.URLParam(r, "channel_id"), act.user.ID, page)
	writeMessagePage(w, messages, err)
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Body            string `json:"body"`
		QuotedMessageID string `json:"quoted_message_id"`
		Nonce           string `json:"nonce"`
		TopicID         string `json:"topic_id"`
		UploadID        string `json:"upload_id"`
		Kind            string `json:"kind"`
		TurnID          string `json:"turn_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	kind, turnID, ok := s.resolveMessageKind(w, act, body.Kind, body.TurnID)
	if !ok {
		return
	}
	if !s.requireBotChannelWorkspace(w, r, act, chi.URLParam(r, "channel_id")) {
		return
	}
	if !s.requireCreateUpload(w, r, act, body.UploadID, body.Nonce, chi.URLParam(r, "channel_id"), "") {
		return
	}
	message, event, err := s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: act.user.ID, Body: body.Body, QuotedMessageID: optionalString(body.QuotedMessageID), Nonce: body.Nonce, TopicID: body.TopicID, UploadID: body.UploadID, Kind: kind, TurnID: turnID})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
		if !store.IsActivityMessageKind(message.Kind) {
			s.notifyMessageCreated(r.Context(), message, event.MentionedUserIDs)
		}
	}
	writeMessageCreateResult(w, message, event, err)
}

// resolveMessageKind validates a caller-supplied message kind and turn_id and
// enforces the activity authorization contract:
//
//   - an empty/'message' kind is always allowed and returned as 'message',
//   - an unknown kind is a 400,
//   - an ordinary 'message' MUST NOT carry a turn_id (400); turn_id correlates
//     agent activity rows only, so a non-empty value on an ordinary message is
//     a client contract violation and fails closed,
//   - an activity kind (agent_commentary/agent_tool) requires a BOT token that
//     carries agent_activity:write; a human session always gets 403 and a bot
//     without the scope gets 403, and may carry a turn_id.
//
// It writes the error response itself and returns ok=false when the request
// must not proceed. On success it returns the normalized kind and the turn_id
// that should be persisted.
func (s *Server) resolveMessageKind(w http.ResponseWriter, act actor, rawKind, rawTurnID string) (string, string, bool) {
	kind, err := store.NormalizeMessageKind(rawKind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return "", "", false
	}
	if !store.IsActivityMessageKind(kind) {
		if rawTurnID != "" {
			writeError(w, http.StatusBadRequest, store.ErrTurnIDNotAllowed)
			return "", "", false
		}
		return kind, "", true
	}
	if act.botTokenID == "" {
		writeError(w, http.StatusForbidden, errors.New("agent activity messages require a bot token"))
		return "", "", false
	}
	if err := act.requireScope(store.AgentActivityWriteScope); err != nil {
		writeError(w, http.StatusForbidden, err)
		return "", "", false
	}
	return kind, rawTurnID, true
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	message, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:read")
	if !ok {
		return
	}
	if act.botTokenID == "" {
		message, err = s.store.GetMessage(r.Context(), chi.URLParam(r, "message_id"), act.user.ID)
	}
	writeResult(w, map[string]any{"message": message}, err)
}

func (s *Server) getMessageByNonce(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-ClickClack-Message-Nonce", "supported")
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace_id is required"))
		return
	}
	nonce, err := store.NormalizeClientNonce(r.URL.Query().Get("nonce"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if nonce == "" {
		writeError(w, http.StatusBadRequest, errors.New("nonce is required"))
		return
	}
	if !s.authorizeWorkspaceAccess(w, r, act, workspaceID) {
		return
	}
	message, err := s.store.GetMessageByNonce(r.Context(), act.user.ID, nonce)
	switch {
	case err == nil && message.WorkspaceID != workspaceID:
		writeStoreError(w, store.ErrClientNonceConflict)
	case err == nil:
		if !requireBotMessageDirectScope(w, act, message, "dms:write") {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": message})
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, err)
	default:
		writeStoreError(w, err)
	}
}

func (s *Server) ensureMessageRoute(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("threads:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	messageID := chi.URLParam(r, "message_id")
	if _, ok := s.requireBotMessageResource(w, r, act, messageID, "dms:read"); !ok {
		return
	}
	message, err := s.store.EnsureMessageRouteID(r.Context(), act.user.ID, messageID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrModerationRestricted) {
		writeError(w, http.StatusNotFound, errors.New("message not found"))
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": message})
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("threads:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:read"); !ok {
		return
	}
	if r.URL.Query().Has("mode") {
		writeError(w, http.StatusBadRequest, errors.New("use latest or a thread sequence cursor"))
		return
	}
	req, err := parseMessagePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	latest := strings.TrimSpace(r.URL.Query().Get("latest"))
	if latest != "" && latest != "true" && latest != "false" {
		writeError(w, http.StatusBadRequest, errors.New("latest must be true or false"))
		return
	}
	page, err := s.store.GetThreadPage(r.Context(), chi.URLParam(r, "message_id"), act.user.ID, store.ThreadPageRequest{MessagePageRequest: req, Latest: latest == "true"})
	writeResult(w, page, err)
}

func (s *Server) createThreadReply(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("threads:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Body            string `json:"body"`
		QuotedMessageID string `json:"quoted_message_id"`
		Nonce           string `json:"nonce"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:write"); !ok {
		return
	}
	message, state, events, err := s.store.CreateThreadReply(r.Context(), store.CreateThreadReplyInput{RootMessageID: chi.URLParam(r, "message_id"), AuthorID: act.user.ID, Body: body.Body, QuotedMessageID: optionalString(body.QuotedMessageID), Nonce: body.Nonce})
	if err == nil && len(events) > 0 {
		s.publishEvents(r.Context(), events)
		s.notifyMessageCreated(r.Context(), message, messageEventMentionedUserIDs(events))
	}
	writeThreadReplyCreateResult(w, message, state, events, err)
}

func (s *Server) addReaction(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:write"); !ok {
		return
	}
	event, err := s.store.AddReaction(r.Context(), store.CreateReactionInput{MessageID: chi.URLParam(r, "message_id"), UserID: act.user.ID, Emoji: body.Emoji})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	s.writeReactionMutationResult(w, r, http.StatusCreated, act.user.ID, chi.URLParam(r, "message_id"), event, err)
}

func (s *Server) removeReaction(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:write"); !ok {
		return
	}
	emoji := chi.URLParam(r, "emoji")
	// Chi routes on RawPath when present; otherwise the parameter is already decoded.
	if r.URL.RawPath != "" {
		emoji, err = url.PathUnescape(emoji)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	event, err := s.store.RemoveReaction(r.Context(), store.CreateReactionInput{MessageID: chi.URLParam(r, "message_id"), UserID: act.user.ID, Emoji: emoji})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
	}
	s.writeReactionMutationResult(w, r, http.StatusOK, act.user.ID, chi.URLParam(r, "message_id"), event, err)
}
