package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) mattermostWebhook(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.requireBotChannelWorkspace(w, r, act, chi.URLParam(r, "channel_id")) {
		return
	}
	message, event, err := s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: act.user.ID, Body: body.Text})
	if err == nil {
		s.publishEvent(r.Context(), event)
		s.notifyMessageCreated(r.Context(), message, event.MentionedUserIDs)
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{"message": message, "event": event}, err)
}

func (s *Server) slashCommand(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.requireBotChannelWorkspace(w, r, act, chi.URLParam(r, "channel_id")) {
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	command := strings.TrimSpace(r.FormValue("command"))
	if text == "" && command == "" {
		writeError(w, http.StatusBadRequest, errors.New("slash command text is required"))
		return
	}
	registered, err := s.store.GetSlashCommandForChannel(r.Context(), chi.URLParam(r, "channel_id"), command, act.user.ID)
	if err == nil {
		s.invokeRegisteredSlashCommand(w, r, act, registered, text)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, err)
		return
	}
	body := strings.TrimSpace(command + " " + text)
	message, event, err := s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: act.user.ID, Body: body})
	if err == nil {
		s.publishEvent(r.Context(), event)
		s.notifyMessageCreated(r.Context(), message, event.MentionedUserIDs)
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{
		"response_type": "in_channel",
		"text":          message.Body,
		"message":       message,
		"event":         event,
	}, err)
}

func (s *Server) invokeRegisteredSlashCommand(w http.ResponseWriter, r *http.Request, act actor, command store.SlashCommand, text string) {
	payload := map[string]any{
		"command_id":   command.ID,
		"command":      command.Command,
		"text":         text,
		"workspace_id": command.WorkspaceID,
		"channel_id":   chi.URLParam(r, "channel_id"),
		"user_id":      act.user.ID,
		"bot_user_id":  command.BotUserID,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	invocation, err := s.store.CreateSlashCommandInvocation(r.Context(), store.CreateSlashCommandInvocationInput{
		CommandID:   command.ID,
		WorkspaceID: command.WorkspaceID,
		ChannelID:   chi.URLParam(r, "channel_id"),
		UserID:      act.user.ID,
		Text:        text,
		PayloadJSON: string(payloadJSON),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	payload["trigger_id"] = invocation.ID
	payloadJSON, err = json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	status, responseBody, callbackErr := s.postSlashCallback(r.Context(), command, payloadJSON)
	invokeErr := ""
	if callbackErr != nil {
		invokeErr = callbackErr.Error()
	}
	_, _ = s.store.CompleteSlashCommandInvocation(r.Context(), invocation.ID, status, responseBody, invokeErr)
	if callbackErr != nil {
		writeError(w, http.StatusBadGateway, callbackErr)
		return
	}
	var callback struct {
		ResponseType string `json:"response_type"`
		Text         string `json:"text"`
	}
	if err := json.Unmarshal([]byte(responseBody), &callback); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	callback.Text = strings.TrimSpace(callback.Text)
	if callback.ResponseType == "" {
		callback.ResponseType = "in_channel"
	}
	var message store.Message
	var event store.Event
	if callback.Text != "" && callback.ResponseType == "in_channel" {
		message, event, err = s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: command.BotUserID, Body: callback.Text})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.publishEvent(r.Context(), event)
		s.notifyMessageCreated(r.Context(), message, event.MentionedUserIDs)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"response_type": callback.ResponseType,
		"text":          callback.Text,
		"message":       message,
		"event":         event,
		"invocation":    invocation,
	})
}
