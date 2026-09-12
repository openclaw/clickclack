package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	nonce, err := store.NormalizeClientNonce(r.URL.Query().Get("nonce"))
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
		s.publishEvent(r.Context(), event)
	}
	writeResult(w, map[string]any{"ok": true}, err)
}
