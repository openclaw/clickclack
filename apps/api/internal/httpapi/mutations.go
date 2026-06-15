package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("channels:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Archived *bool  `json:"archived"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.requireBotChannelWorkspace(w, r, act, chi.URLParam(r, "channel_id")) {
		return
	}
	channel, event, err := s.store.UpdateChannel(r.Context(), store.UpdateChannelInput{ChannelID: chi.URLParam(r, "channel_id"), UserID: act.user.ID, Name: body.Name, Kind: body.Kind, Archived: body.Archived})
	if err == nil {
		s.publishEvent(r.Context(), event)
	}
	writeResult(w, map[string]any{"channel": channel, "event": event}, err)
}

func (s *Server) updateMessage(w http.ResponseWriter, r *http.Request) {
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
		Body string `json:"body"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:write"); !ok {
		return
	}
	message, event, err := s.store.UpdateMessage(r.Context(), store.UpdateMessageInput{MessageID: chi.URLParam(r, "message_id"), UserID: act.user.ID, Body: body.Body})
	if err == nil {
		s.publishEvent(r.Context(), event)
	}
	writeResult(w, map[string]any{"message": message, "event": event}, err)
}

func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
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
	message, event, err := s.store.DeleteMessage(r.Context(), store.DeleteMessageInput{MessageID: chi.URLParam(r, "message_id"), UserID: act.user.ID})
	if err == nil {
		s.publishEvent(r.Context(), event)
	}
	writeResult(w, map[string]any{"message": message, "event": event}, err)
}

func (s *Server) publishEphemeral(w http.ResponseWriter, r *http.Request) {
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
		WorkspaceID          string         `json:"workspace_id"`
		ChannelID            string         `json:"channel_id"`
		DirectConversationID string         `json:"direct_conversation_id"`
		Type                 string         `json:"type"`
		Payload              map[string]any `json:"payload"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// agent.progress is the source-bridge progress tick. It rides the same
	// ephemeral transport and message-write authorization as typing/presence,
	// but is bot-token-only and REQUIRED to name a single concrete target so a
	// private turn can never broadcast to a whole workspace. The recipient list
	// is always server-derived below; the relay never supplies recipients
	// directly.
	isAgentProgress := body.Type == "agent.progress"
	if body.Type != "typing.started" && body.Type != "typing.stopped" &&
		body.Type != "presence.changed" && !isAgentProgress {
		writeError(w, http.StatusBadRequest, errors.New("unsupported ephemeral event type"))
		return
	}
	if isAgentProgress {
		// Human sessions never publish agent progress. A bot with normal write
		// access can publish progress for the channel or DM it is allowed to use.
		if act.botTokenID == "" {
			writeError(w, http.StatusForbidden, errors.New("agent.progress requires a bot token"))
			return
		}
	}
	if err := act.requireWorkspace(body.WorkspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.store.GetWorkspace(r.Context(), body.WorkspaceID, act.user.ID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if body.Payload == nil {
		body.Payload = map[string]any{}
	}
	channelID := strings.TrimSpace(body.ChannelID)
	directConversationID := strings.TrimSpace(body.DirectConversationID)
	if (body.Type == "typing.started" || body.Type == "typing.stopped") && channelID == "" && directConversationID == "" {
		writeError(w, http.StatusBadRequest, errors.New("typing events require channel_id or direct_conversation_id"))
		return
	}
	// S1: a progress frame MUST target exactly one concrete surface. An empty
	// target would otherwise fall through to the workspace-wide branch below and
	// leak thinking/command-output/patch detail to every member. (channel_id and
	// direct_conversation_id are also mutually exclusive, enforced just below.)
	if isAgentProgress && channelID == "" && directConversationID == "" {
		writeError(w, http.StatusBadRequest, errors.New("agent.progress requires channel_id or direct_conversation_id"))
		return
	}
	var recipientUserIDs []string
	if directConversationID != "" {
		if channelID != "" {
			writeError(w, http.StatusBadRequest, errors.New("channel_id and direct_conversation_id are mutually exclusive"))
			return
		}
		dm, err := s.store.GetDirectConversation(r.Context(), directConversationID, act.user.ID)
		if err != nil || dm.WorkspaceID != body.WorkspaceID {
			if err == nil {
				err = errors.New("direct conversation is not in this workspace")
			}
			writeError(w, http.StatusForbidden, err)
			return
		}
		if act.botTokenID != "" {
			if err := act.requireScope("dms:write"); err != nil {
				writeError(w, http.StatusForbidden, err)
				return
			}
		}
		recipientUserIDs = make([]string, 0, len(dm.Members))
		for _, member := range dm.Members {
			recipientUserIDs = append(recipientUserIDs, member.ID)
		}
		body.Payload["direct_conversation_id"] = directConversationID
		delete(body.Payload, "channel_id")
		body.DirectConversationID = directConversationID
	} else if channelID != "" {
		channel, err := s.store.GetChannel(r.Context(), channelID, act.user.ID)
		if err != nil || channel.WorkspaceID != body.WorkspaceID {
			if err == nil {
				err = errors.New("channel is not in this workspace")
			}
			writeError(w, http.StatusForbidden, err)
			return
		}
		body.Payload["channel_id"] = channelID
		delete(body.Payload, "direct_conversation_id")
		body.ChannelID = channelID
	} else {
		delete(body.Payload, "channel_id")
		delete(body.Payload, "direct_conversation_id")
	}
	if err := s.store.CanPublishEphemeral(r.Context(), body.WorkspaceID, channelID, directConversationID, act.user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	body.Payload["user_id"] = act.user.ID
	event := store.Event{
		ID:               "eph_" + time.Now().UTC().Format("20060102150405.000000000"),
		Type:             body.Type,
		WorkspaceID:      body.WorkspaceID,
		ChannelID:        channelID,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		Payload:          body.Payload,
		RecipientUserIDs: recipientUserIDs,
	}
	s.publishEvent(r.Context(), event)
	writeJSON(w, http.StatusAccepted, map[string]any{"event": event})
}
