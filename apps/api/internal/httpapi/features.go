package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func formInt(r *http.Request, key string) int {
	v, err := strconv.Atoi(r.FormValue(key))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func (s *Server) markChannelRead(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Seq int64 `json:"seq"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	receipt, event, err := s.store.MarkChannelRead(r.Context(), chi.URLParam(r, "channel_id"), user.ID, body.Seq)
	if err == nil && event.ID != "" {
		s.hub.Publish(event)
	}
	writeResult(w, map[string]any{"receipt": receipt}, err)
}

func (s *Server) markDirectRead(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Seq int64 `json:"seq"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	receipt, event, err := s.store.MarkDirectRead(r.Context(), chi.URLParam(r, "conversation_id"), user.ID, body.Seq)
	if err == nil && event.ID != "" {
		s.hub.Publish(event)
	}
	writeResult(w, map[string]any{"receipt": receipt}, err)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	results, err := s.store.SearchMessages(r.Context(), r.URL.Query().Get("workspace_id"), user.ID, r.URL.Query().Get("q"), queryInt(r, "limit", 50))
	writeResult(w, map[string]any{"results": results}, err)
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if s.uploadDir == "" {
		writeError(w, http.StatusInternalServerError, errors.New("uploads are not configured"))
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := r.FormValue("workspace_id")
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	if err := os.MkdirAll(s.uploadDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tmp, err := os.CreateTemp(s.uploadDir, "upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tmp.Close()
	size, err := io.Copy(tmp, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	upload, err := s.store.CreateUpload(r.Context(), store.CreateUploadInput{
		WorkspaceID: workspaceID,
		OwnerID:     user.ID,
		Filename:    filepath.Base(header.Filename),
		ContentType: contentType,
		ByteSize:    size,
		Width:       formInt(r, "width"),
		Height:      formInt(r, "height"),
		DurationMS:  formInt(r, "duration_ms"),
		StoragePath: tmp.Name(),
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"upload": upload}, err)
}

func (s *Server) getUpload(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	upload, err := s.store.GetUpload(r.Context(), chi.URLParam(r, "upload_id"), user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	http.ServeFile(w, r, upload.StoragePath)
}

func (s *Server) attachUpload(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		UploadID string `json:"upload_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	err = s.store.AttachUpload(r.Context(), store.AttachUploadInput{MessageID: chi.URLParam(r, "message_id"), UploadID: body.UploadID, UserID: user.ID})
	writeResult(w, map[string]any{"ok": true}, err)
}

func (s *Server) listDirectConversations(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	items, err := s.store.ListDirectConversations(r.Context(), r.URL.Query().Get("workspace_id"), user.ID)
	writeResult(w, map[string]any{"conversations": items}, err)
}

func (s *Server) createDirectConversation(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		WorkspaceID string   `json:"workspace_id"`
		MemberIDs   []string `json:"member_ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dm, err := s.store.CreateDirectConversation(r.Context(), store.CreateDirectConversationInput{WorkspaceID: body.WorkspaceID, UserID: user.ID, MemberIDs: body.MemberIDs})
	writeResultStatus(w, http.StatusCreated, map[string]any{"conversation": dm}, err)
}

func (s *Server) listDirectMessages(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	messages, err := s.store.ListDirectMessages(r.Context(), chi.URLParam(r, "conversation_id"), user.ID, queryInt64(r, "after_seq", 0), queryInt(r, "limit", 100))
	writeResult(w, map[string]any{"messages": messages}, err)
}

func (s *Server) createDirectMessage(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Body            string `json:"body"`
		QuotedMessageID string `json:"quoted_message_id"`
		Nonce           string `json:"nonce"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	message, event, err := s.store.CreateDirectMessage(r.Context(), store.CreateDirectMessageInput{ConversationID: chi.URLParam(r, "conversation_id"), AuthorID: user.ID, Body: body.Body, QuotedMessageID: optionalString(body.QuotedMessageID), Nonce: body.Nonce})
	if err == nil && event.ID != "" {
		s.hub.Publish(event)
		s.notifyMessageCreated(r.Context(), message)
	}
	writeMessageCreateResult(w, message, event, err)
}

func (s *Server) mattermostWebhook(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	message, event, err := s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: user.ID, Body: body.Text})
	if err == nil {
		s.hub.Publish(event)
		s.notifyMessageCreated(r.Context(), message)
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{"message": message, "event": event}, err)
}

func (s *Server) slashCommand(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	command := strings.TrimSpace(r.FormValue("command"))
	if text == "" && command == "" {
		writeError(w, http.StatusBadRequest, errors.New("slash command text is required"))
		return
	}
	body := strings.TrimSpace(command + " " + text)
	message, event, err := s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: user.ID, Body: body})
	if err == nil {
		s.hub.Publish(event)
		s.notifyMessageCreated(r.Context(), message)
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{
		"response_type": "in_channel",
		"text":          message.Body,
		"message":       message,
		"event":         event,
	}, err)
}
