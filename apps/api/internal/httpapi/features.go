package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/uploadstore"
)

const (
	maxUploadBytes       = 64 << 20
	uploadCleanupTimeout = 5 * time.Second
	uploadNonceRetryWait = 25 * time.Millisecond
	uploadNonceRetryMax  = 250 * time.Millisecond
)

func formInt(values map[string]string, key string) int {
	v, err := strconv.Atoi(values[key])
	if err != nil || v < 0 {
		return 0
	}
	return v
}

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

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	results, err := s.store.SearchMessages(r.Context(), workspaceID, r.URL.Query().Get("channel_id"), act.user.ID, r.URL.Query().Get("q"), queryInt(r, "limit", 50))
	writeResult(w, map[string]any{"results": results}, err)
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if s.uploadStorage == nil {
		writeError(w, http.StatusInternalServerError, errors.New("uploads are not configured"))
		return
	}
	if err := act.requireScope("uploads:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	nonce, err := normalizeClientNonce(r.URL.Query().Get("nonce"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if nonce != "" && workspaceID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace_id query parameter is required with nonce"))
		return
	}
	if workspaceID != "" {
		if !s.authorizeWorkspaceAccess(w, r, act, workspaceID) {
			return
		}
		if nonce != "" {
			existing, err := s.store.GetUploadByNonce(r.Context(), act.user.ID, nonce)
			switch {
			case err == nil:
				if existing.WorkspaceID != workspaceID {
					writeStoreError(w, store.ErrUploadNonceConflict)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"upload": existing})
				return
			case !errors.Is(err, sql.ErrNoRows):
				writeStoreError(w, err)
				return
			}
		} else if _, ok := s.checkUploadQuota(w, r, act, workspaceID); !ok {
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	fields := map[string]string{}
	var upload store.CreateUploadInput
	var savedPath string
	var reservationID string
	committed := false
	defer func() {
		if savedPath != "" && !committed {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), uploadCleanupTimeout)
			defer cancel()
			_ = s.uploadStorage.Delete(cleanupCtx, savedPath)
		}
		if reservationID != "" && !committed {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), uploadCleanupTimeout)
			defer cancel()
			_ = s.store.ReleaseUploadQuotaReservation(cleanupCtx, reservationID, act.user.ID)
		}
	}()
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeUploadBodyError(w, err, http.StatusBadRequest)
			return
		}
		name := part.FormName()
		if name == "" {
			continue
		}
		if name != "file" {
			body, err := io.ReadAll(io.LimitReader(part, 1024))
			if err != nil {
				writeUploadBodyError(w, err, http.StatusBadRequest)
				return
			}
			fields[name] = string(body)
			if name == "workspace_id" && workspaceID == "" {
				workspaceID = strings.TrimSpace(fields[name])
				var ok bool
				_, ok = s.authorizeUploadWorkspace(w, r, act, workspaceID)
				if !ok {
					return
				}
			} else if name == "workspace_id" && strings.TrimSpace(fields[name]) != "" && strings.TrimSpace(fields[name]) != workspaceID {
				writeError(w, http.StatusBadRequest, errors.New("workspace_id does not match query"))
				return
			}
			continue
		}
		if workspaceID == "" {
			writeError(w, http.StatusBadRequest, errors.New("workspace_id must precede file or be provided as a query parameter"))
			return
		}
		if upload.StoragePath != "" {
			writeError(w, http.StatusBadRequest, errors.New("only one file is supported"))
			return
		}
		contentType := part.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		reservation, replayed, err := s.reserveUploadQuota(r.Context(), workspaceID, act.user.ID, nonce)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if replayed != nil {
			writeJSON(w, http.StatusOK, map[string]any{"upload": *replayed})
			return
		}
		reservationID = reservation.ID
		saved, err := s.uploadStorage.Save(r.Context(), &uploadQuotaReader{reader: part, remaining: reservation.ByteSize}, uploadstore.SaveOptions{ContentType: contentType})
		if err != nil {
			writeUploadBodyError(w, err, http.StatusInternalServerError)
			return
		}
		savedPath = saved.Path
		upload = store.CreateUploadInput{
			WorkspaceID: workspaceID,
			OwnerID:     act.user.ID,
			Nonce:       nonce,
			Filename:    filepath.Base(part.FileName()),
			ContentType: contentType,
			ByteSize:    saved.ByteSize,
			Width:       formInt(fields, "width"),
			Height:      formInt(fields, "height"),
			DurationMS:  formInt(fields, "duration_ms"),
			StoragePath: saved.Path,
		}
	}
	if upload.StoragePath == "" {
		writeError(w, http.StatusBadRequest, errors.New("file is required"))
		return
	}
	upload.Width = formInt(fields, "width")
	upload.Height = formInt(fields, "height")
	upload.DurationMS = formInt(fields, "duration_ms")
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	created, err := s.store.CreateReservedUpload(r.Context(), reservationID, upload)
	if err == nil {
		committed = true
		reservationID = ""
		writeJSON(w, http.StatusCreated, map[string]any{"upload": created})
		return
	}
	if nonce != "" {
		// A concurrent request may have committed this nonce after the early lookup.
		// Keep committed false so the deferred cleanup discards this request's object.
		existing, lookupErr := s.store.GetUploadByNonce(r.Context(), act.user.ID, nonce)
		switch {
		case lookupErr == nil && existing.WorkspaceID == workspaceID:
			writeJSON(w, http.StatusOK, map[string]any{"upload": existing})
			return
		case lookupErr == nil:
			writeStoreError(w, store.ErrUploadNonceConflict)
			return
		case !errors.Is(lookupErr, sql.ErrNoRows):
			writeStoreError(w, lookupErr)
			return
		}
	}
	writeStoreError(w, err)
}

func (s *Server) reserveUploadQuota(ctx context.Context, workspaceID, ownerID, nonce string) (store.UploadQuotaReservation, *store.Upload, error) {
	retryWait := uploadNonceRetryWait
	for {
		reservation, err := s.store.ReserveUploadQuota(ctx, workspaceID, ownerID, nonce, int64(maxUploadBytes))
		if err == nil {
			return reservation, nil, nil
		}
		if nonce == "" || !errors.Is(err, store.ErrUploadNonceInProgress) {
			return store.UploadQuotaReservation{}, nil, err
		}
		existing, lookupErr := s.store.GetUploadByNonce(ctx, ownerID, nonce)
		switch {
		case lookupErr == nil && existing.WorkspaceID != workspaceID:
			return store.UploadQuotaReservation{}, nil, store.ErrUploadNonceConflict
		case lookupErr == nil:
			return store.UploadQuotaReservation{}, &existing, nil
		case !errors.Is(lookupErr, sql.ErrNoRows):
			return store.UploadQuotaReservation{}, nil, lookupErr
		}
		timer := time.NewTimer(retryWait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return store.UploadQuotaReservation{}, nil, ctx.Err()
		case <-timer.C:
		}
		retryWait = min(retryWait*2, uploadNonceRetryMax)
	}
}

type uploadQuotaReader struct {
	reader    io.Reader
	remaining int64
}

func (r *uploadQuotaReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining <= 0 {
		if len(p) > 1 {
			p = p[:1]
		}
		n, err := r.reader.Read(p)
		if n > 0 {
			return 0, store.ErrUploadQuotaExceeded
		}
		return n, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (s *Server) authorizeUploadWorkspace(w http.ResponseWriter, r *http.Request, act actor, workspaceID string) (store.UploadQuota, bool) {
	if !s.authorizeWorkspaceAccess(w, r, act, workspaceID) {
		return store.UploadQuota{}, false
	}
	return s.checkUploadQuota(w, r, act, workspaceID)
}

func (s *Server) checkUploadQuota(w http.ResponseWriter, r *http.Request, act actor, workspaceID string) (store.UploadQuota, bool) {
	quota, err := s.store.UploadQuota(r.Context(), workspaceID, act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return store.UploadQuota{}, false
	}
	if err := quota.CanFit(0); err != nil {
		writeStoreError(w, err)
		return store.UploadQuota{}, false
	}
	return quota, true
}

func (s *Server) authorizeWorkspaceAccess(w http.ResponseWriter, r *http.Request, act actor, workspaceID string) bool {
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return false
	}
	if _, err := s.store.GetWorkspace(r.Context(), workspaceID, act.user.ID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return false
	}
	return true
}

func normalizeClientNonce(value string) (string, error) {
	nonce := strings.TrimSpace(value)
	if utf8.RuneCountInString(nonce) > 128 {
		return "", errors.New("nonce is too long")
	}
	return nonce, nil
}

func writeUploadBodyError(w http.ResponseWriter, err error, fallbackStatus int) {
	if errors.Is(err, store.ErrUploadQuotaExceeded) {
		writeStoreError(w, err)
		return
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeError(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	writeError(w, fallbackStatus, err)
}

func (s *Server) getUploadByNonce(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-ClickClack-Upload-Nonce", "supported")
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("uploads:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace_id is required"))
		return
	}
	nonce, err := normalizeClientNonce(r.URL.Query().Get("nonce"))
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
	upload, err := s.store.GetUploadByNonce(r.Context(), act.user.ID, nonce)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if upload.WorkspaceID != workspaceID {
		writeStoreError(w, store.ErrUploadNonceConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upload": upload})
}

func (s *Server) getUpload(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if s.uploadStorage == nil {
		writeError(w, http.StatusInternalServerError, errors.New("uploads are not configured"))
		return
	}
	upload, err := s.store.GetUpload(r.Context(), chi.URLParam(r, "upload_id"), act.user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !s.requireBotUploadResource(w, r, act, upload, "") {
		return
	}
	setUploadResponseHeaders(w, upload)
	err = s.uploadStorage.ServeHTTP(w, r, uploadstore.Object{
		Path:        upload.StoragePath,
		Filename:    upload.Filename,
		ContentType: safeUploadContentType(upload.ContentType),
		ByteSize:    upload.ByteSize,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, uploadstore.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
	}
}

func setUploadResponseHeaders(w http.ResponseWriter, upload store.Upload) {
	contentType := safeUploadContentType(upload.ContentType)
	disposition := "attachment"
	if isInlineUploadContentType(contentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": upload.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
}

func safeUploadContentType(value string) string {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if strings.HasPrefix(contentType, "audio/") {
		return contentType
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4", "video/webm", "text/plain", "application/pdf":
		return contentType
	default:
		return "application/octet-stream"
	}
}

func isInlineUploadContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") || strings.HasPrefix(contentType, "audio/") || contentType == "application/pdf" || contentType == "text/plain"
}

func (s *Server) attachUpload(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		UploadID string `json:"upload_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := act.requireScope("uploads:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	message, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:write")
	if !ok {
		return
	}
	if message.AuthorID != act.user.ID {
		writeError(w, http.StatusForbidden, errors.New("message attachments can only be changed by the message author"))
		return
	}
	upload, err := s.store.GetUpload(r.Context(), body.UploadID, act.user.ID)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.requireBotUploadResource(w, r, act, upload, message.ID) {
		return
	}
	event, err := s.store.AttachUpload(r.Context(), store.AttachUploadInput{MessageID: chi.URLParam(r, "message_id"), UploadID: body.UploadID, UserID: act.user.ID})
	if err == nil && event.ID != "" {
		s.hub.Publish(event)
	}
	writeResult(w, map[string]any{"ok": true}, err)
}

func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage bots"))
		return
	}
	bots, err := s.store.ListBots(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"bots": bots}, err)
}

func (s *Server) listMyBots(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list owned bots"))
		return
	}
	bots, err := s.store.ListBotsOwnedBy(r.Context(), act.user.ID)
	writeResult(w, map[string]any{"bots": bots}, err)
}

func (s *Server) createBot(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create bots"))
		return
	}
	var body struct {
		OwnerUserID string   `json:"owner_user_id"`
		DisplayName string   `json:"display_name"`
		Handle      string   `json:"handle"`
		AvatarURL   string   `json:"avatar_url"`
		TokenName   string   `json:"token_name"`
		Scopes      []string `json:"scopes"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	bot, token, err := s.store.CreateBot(r.Context(), store.CreateBotInput{
		WorkspaceID: chi.URLParam(r, "workspace_id"),
		OwnerUserID: body.OwnerUserID,
		DisplayName: body.DisplayName,
		Handle:      body.Handle,
		AvatarURL:   body.AvatarURL,
		TokenName:   body.TokenName,
		Scopes:      body.Scopes,
		CreatedBy:   act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"bot": bot, "bot_token": token}, err)
}

func (s *Server) listBotTokens(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage bot tokens"))
		return
	}
	tokens, err := s.store.ListBotTokens(r.Context(), chi.URLParam(r, "bot_user_id"), act.user.ID)
	writeResult(w, map[string]any{"bot_tokens": tokens}, err)
}

func (s *Server) listWorkspaceBotTokens(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage bot tokens"))
		return
	}
	tokens, err := s.store.ListBotTokensForWorkspace(r.Context(), chi.URLParam(r, "workspace_id"), chi.URLParam(r, "bot_user_id"), act.user.ID)
	writeResult(w, map[string]any{"bot_tokens": tokens}, err)
}

func (s *Server) createBotToken(w http.ResponseWriter, r *http.Request) {
	s.createBotTokenForWorkspace(w, r, "")
}

func (s *Server) createWorkspaceBotToken(w http.ResponseWriter, r *http.Request) {
	s.createBotTokenForWorkspace(w, r, chi.URLParam(r, "workspace_id"))
}

func (s *Server) createBotTokenForWorkspace(w http.ResponseWriter, r *http.Request, workspaceID string) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create bot tokens"))
		return
	}
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, err := s.store.CreateBotToken(r.Context(), store.CreateBotTokenInput{
		WorkspaceID: workspaceID,
		BotUserID:   chi.URLParam(r, "bot_user_id"),
		Name:        body.Name,
		Scopes:      body.Scopes,
		CreatedBy:   act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"bot_token": token}, err)
}

func (s *Server) removeBotFromWorkspace(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot remove bots from workspaces"))
		return
	}
	err = s.store.RemoveBotFromWorkspace(r.Context(), chi.URLParam(r, "workspace_id"), chi.URLParam(r, "bot_user_id"), act.user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeBotToken(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke bot tokens"))
		return
	}
	token, err := s.store.RevokeBotToken(r.Context(), chi.URLParam(r, "token_id"), act.user.ID)
	writeResult(w, map[string]any{"bot_token": token}, err)
}

func (s *Server) listAppInstallations(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage app installations"))
		return
	}
	installations, err := s.store.ListAppInstallations(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"app_installations": installations}, err)
}

func (s *Server) createAppInstallation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create app installations"))
		return
	}
	var body struct {
		AppSlug     string         `json:"app_slug"`
		DisplayName string         `json:"display_name"`
		BotUserID   string         `json:"bot_user_id"`
		Config      map[string]any `json:"config"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	installation, err := s.store.CreateAppInstallation(r.Context(), store.CreateAppInstallationInput{
		WorkspaceID: chi.URLParam(r, "workspace_id"),
		AppSlug:     body.AppSlug,
		DisplayName: body.DisplayName,
		BotUserID:   body.BotUserID,
		Config:      body.Config,
		CreatedBy:   act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"app_installation": installation}, err)
}

func (s *Server) revokeAppInstallation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke app installations"))
		return
	}
	installation, err := s.store.RevokeAppInstallation(r.Context(), chi.URLParam(r, "installation_id"), act.user.ID)
	writeResult(w, map[string]any{"app_installation": installation}, err)
}

func (s *Server) listSlashCommands(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage slash commands"))
		return
	}
	commands, err := s.store.ListSlashCommands(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"slash_commands": commands}, err)
}

func (s *Server) createSlashCommand(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create slash commands"))
		return
	}
	var body struct {
		AppInstallationID string `json:"app_installation_id"`
		Command           string `json:"command"`
		Description       string `json:"description"`
		CallbackURL       string `json:"callback_url"`
		BotUserID         string `json:"bot_user_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	command, err := s.store.CreateSlashCommand(r.Context(), store.CreateSlashCommandInput{
		WorkspaceID:       chi.URLParam(r, "workspace_id"),
		AppInstallationID: body.AppInstallationID,
		Command:           body.Command,
		Description:       body.Description,
		CallbackURL:       body.CallbackURL,
		BotUserID:         body.BotUserID,
		CreatedBy:         act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"slash_command": command}, err)
}

func (s *Server) revokeSlashCommand(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke slash commands"))
		return
	}
	command, err := s.store.RevokeSlashCommand(r.Context(), chi.URLParam(r, "command_id"), act.user.ID)
	writeResult(w, map[string]any{"slash_command": command}, err)
}

func (s *Server) listEventSubscriptions(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage event subscriptions"))
		return
	}
	subscriptions, err := s.store.ListEventSubscriptions(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"event_subscriptions": subscriptions}, err)
}

func (s *Server) createEventSubscription(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create event subscriptions"))
		return
	}
	var body struct {
		AppInstallationID string   `json:"app_installation_id"`
		EventTypes        []string `json:"event_types"`
		CallbackURL       string   `json:"callback_url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	subscription, err := s.store.CreateEventSubscription(r.Context(), store.CreateEventSubscriptionInput{
		WorkspaceID:       chi.URLParam(r, "workspace_id"),
		AppInstallationID: body.AppInstallationID,
		EventTypes:        body.EventTypes,
		CallbackURL:       body.CallbackURL,
		CreatedBy:         act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"event_subscription": subscription}, err)
}

func (s *Server) revokeEventSubscription(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke event subscriptions"))
		return
	}
	subscription, err := s.store.RevokeEventSubscription(r.Context(), chi.URLParam(r, "subscription_id"), act.user.ID)
	writeResult(w, map[string]any{"event_subscription": subscription}, err)
}

func (s *Server) listEventDeliveryAttempts(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list event delivery attempts"))
		return
	}
	attempts, err := s.store.ListEventDeliveryAttempts(r.Context(), chi.URLParam(r, "subscription_id"), act.user.ID)
	writeResult(w, map[string]any{"event_delivery_attempts": attempts}, err)
}

func (s *Server) listAuditLogEntries(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list audit log entries"))
		return
	}
	entries, err := s.store.ListAuditLogEntries(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID, queryInt(r, "limit", 100))
	writeResult(w, map[string]any{"audit_log_entries": entries}, err)
}

func (s *Server) listConnectedAccounts(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list connected accounts"))
		return
	}
	accounts, err := s.store.ListConnectedAccounts(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"connected_accounts": accounts}, err)
}

func (s *Server) createConnectedAccount(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create connected accounts"))
		return
	}
	var body struct {
		UserID            string         `json:"user_id"`
		Provider          string         `json:"provider"`
		ProviderAccountID string         `json:"provider_account_id"`
		DisplayName       string         `json:"display_name"`
		Scopes            []string       `json:"scopes"`
		Metadata          map[string]any `json:"metadata"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	account, err := s.store.CreateConnectedAccount(r.Context(), store.CreateConnectedAccountInput{
		WorkspaceID:       chi.URLParam(r, "workspace_id"),
		UserID:            body.UserID,
		Provider:          body.Provider,
		ProviderAccountID: body.ProviderAccountID,
		DisplayName:       body.DisplayName,
		Scopes:            body.Scopes,
		Metadata:          body.Metadata,
		CreatedBy:         act.user.ID,
	})
	if err == nil {
		s.recordAudit(r.Context(), account.WorkspaceID, act.user.ID, "connected_account.created", "connected_account", account.ID, map[string]any{"provider": account.Provider})
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{"connected_account": account}, err)
}

func (s *Server) revokeConnectedAccount(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke connected accounts"))
		return
	}
	account, err := s.store.RevokeConnectedAccount(r.Context(), chi.URLParam(r, "account_id"), act.user.ID)
	if err == nil {
		s.recordAudit(r.Context(), account.WorkspaceID, act.user.ID, "connected_account.revoked", "connected_account", account.ID, map[string]any{"provider": account.Provider})
	}
	writeResult(w, map[string]any{"connected_account": account}, err)
}

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
	message, event, err := s.store.CreateDirectMessage(r.Context(), store.CreateDirectMessageInput{ConversationID: chi.URLParam(r, "conversation_id"), AuthorID: act.user.ID, Body: body.Body, QuotedMessageID: optionalString(body.QuotedMessageID), Nonce: body.Nonce, Kind: kind, TurnID: turnID})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
		if !store.IsActivityMessageKind(message.Kind) {
			s.notifyMessageCreated(r.Context(), message)
		}
	}
	writeMessageCreateResult(w, message, event, err)
}

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
		s.notifyMessageCreated(r.Context(), message)
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
		writeError(w, http.StatusBadRequest, err)
		return
	}
	body := strings.TrimSpace(command + " " + text)
	message, event, err := s.store.CreateMessage(r.Context(), store.CreateMessageInput{ChannelID: chi.URLParam(r, "channel_id"), AuthorID: act.user.ID, Body: body})
	if err == nil {
		s.publishEvent(r.Context(), event)
		s.notifyMessageCreated(r.Context(), message)
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
		writeError(w, http.StatusBadRequest, err)
		return
	}
	payload["trigger_id"] = invocation.ID
	payloadJSON, err = json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	status, responseBody, callbackErr := postSlashCallback(r.Context(), command, payloadJSON)
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
		s.notifyMessageCreated(r.Context(), message)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"response_type": callback.ResponseType,
		"text":          callback.Text,
		"message":       message,
		"event":         event,
		"invocation":    invocation,
	})
}

func (s *Server) publishEvent(ctx context.Context, event store.Event) {
	s.hub.Publish(event)
	if event.ID == "" || event.Cursor == "" {
		return
	}
	s.deliverEventSubscriptions(ctx, event)
}

func (s *Server) recordAudit(ctx context.Context, workspaceID, actorUserID, action, targetType, targetID string, metadata map[string]any) {
	_, _ = s.store.CreateAuditLogEntry(ctx, store.CreateAuditLogEntryInput{
		WorkspaceID: workspaceID,
		ActorUserID: actorUserID,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		Metadata:    metadata,
	})
}

func (s *Server) publishEvents(ctx context.Context, events []store.Event) {
	for _, event := range events {
		s.publishEvent(ctx, event)
	}
}

func (s *Server) deliverEventSubscriptions(ctx context.Context, event store.Event) {
	subscriptions, err := s.store.ListEventSubscriptionsForEvent(ctx, event)
	if err != nil {
		return
	}
	for _, subscription := range subscriptions {
		payload, err := json.Marshal(map[string]any{
			"subscription_id": subscription.ID,
			"event":           event,
		})
		if err != nil {
			continue
		}
		status, responseBody, deliveryErr := postEventCallback(ctx, subscription, event, payload)
		errorText := ""
		if deliveryErr != nil {
			errorText = deliveryErr.Error()
		}
		_, _ = s.store.CreateEventDeliveryAttempt(ctx, store.CreateEventDeliveryAttemptInput{
			SubscriptionID: subscription.ID,
			EventID:        event.ID,
			WorkspaceID:    event.WorkspaceID,
			EventType:      event.Type,
			RequestJSON:    string(payload),
			ResponseStatus: status,
			ResponseBody:   responseBody,
			Error:          errorText,
		})
	}
}

func postEventCallback(ctx context.Context, subscription store.EventSubscription, event store.Event, payload []byte) (int, string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.CallbackURL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-Timestamp", timestamp)
	req.Header.Set("X-ClickClack-Event-ID", event.ID)
	req.Header.Set("X-ClickClack-Signature", signSlashCallback(subscription.SigningSecret, timestamp, payload))
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, string(body), errors.New("event subscription callback failed")
	}
	return resp.StatusCode, string(body), nil
}

func postSlashCallback(ctx context.Context, command store.SlashCommand, payload []byte) (int, string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, command.CallbackURL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-Timestamp", timestamp)
	req.Header.Set("X-ClickClack-Signature", signSlashCallback(command.SigningSecret, timestamp, payload))
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, string(body), errors.New("slash command callback failed")
	}
	return resp.StatusCode, string(body), nil
}

func signSlashCallback(secret, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
