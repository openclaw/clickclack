package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func readJSON(w http.ResponseWriter, r *http.Request, out any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(out)
	if err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		if _, drainErr := io.Copy(io.Discard, r.Body); drainErr != nil {
			return drainErr
		}
		return errors.New("json request body must contain a single JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeResult(w http.ResponseWriter, body any, err error) {
	writeResultStatus(w, http.StatusOK, body, err)
}

func writeResultStatus(w http.ResponseWriter, status int, body any, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, status, body)
}

func writeMessageCreateResult(w http.ResponseWriter, message store.Message, event store.Event, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	body := map[string]any{"message": message}
	status := http.StatusOK
	if event.ID != "" {
		body["event"] = event
		status = http.StatusCreated
	}
	writeJSON(w, status, body)
}

func writeThreadReplyCreateResult(w http.ResponseWriter, message store.Message, state store.ThreadState, events []store.Event, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := http.StatusOK
	if len(events) > 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"message": message, "thread_state": state, "events": events})
}

func (s *Server) writeReactionMutationResult(w http.ResponseWriter, r *http.Request, changedStatus int, userID, messageID string, event store.Event, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	message, err := s.store.GetMessage(r.Context(), messageID, userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := http.StatusOK
	if event.ID != "" {
		status = changedStatus
	}
	writeJSON(w, status, map[string]any{"event": event, "reactions": message.Reactions})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if errors.Is(err, errSessionLookupUnavailable) {
		status = http.StatusServiceUnavailable
		err = errSessionLookupUnavailable
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		status = http.StatusRequestEntityTooLarge
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrPostRateLimited):
		writeError(w, http.StatusTooManyRequests, err)
	case errors.Is(err, store.ErrUploadQuotaExceeded):
		writeError(w, http.StatusRequestEntityTooLarge, err)
	case errors.Is(err, store.ErrUploadNonceConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrSetupNonceConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrAlreadyPinned):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrPinnedMessageLimit):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrSetupCodeInvalid):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrModerationRestricted):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrNotWorkspaceManager):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrWorkspaceOwnerRequired):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrBotOwnerRequired):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrBotOwnerMembershipRequired):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrBotOwnerCreateRequired):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrMessageNotWritable):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, store.ErrDirectConversationNoActivePeer):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

// optionalString returns a non-empty trimmed pointer or nil. Useful for JSON
// fields that should map to a nullable Go pointer when absent or blank.
func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 32)
	if err != nil {
		return fallback
	}
	return int(value)
}

func parseMessagePageRequest(r *http.Request) (store.MessagePageRequest, error) {
	values := r.URL.Query()
	req := store.MessagePageRequest{Limit: queryInt(r, "limit", 100)}
	cursorCount := 0
	for _, cursor := range []struct {
		key string
		set func(int64)
	}{
		{"before_seq", func(v int64) { req.BeforeSeq = &v }},
		{"after_seq", func(v int64) { req.AfterSeq = &v }},
		{"around_seq", func(v int64) { req.AroundSeq = &v }},
	} {
		raw, ok := values[cursor.key]
		if !ok {
			continue
		}
		cursorCount++
		if len(raw) == 0 || strings.TrimSpace(raw[0]) == "" {
			return req, fmt.Errorf("%w: %s is required", store.ErrInvalidMessagePage, cursor.key)
		}
		value, err := strconv.ParseInt(raw[0], 10, 64)
		if err != nil || value < 0 {
			return req, fmt.Errorf("%w: %s must be a non-negative integer", store.ErrInvalidMessagePage, cursor.key)
		}
		cursor.set(value)
	}
	if cursorCount > 1 {
		return req, fmt.Errorf("%w: before_seq, after_seq, and around_seq are mutually exclusive", store.ErrInvalidMessagePage)
	}
	if mode := values.Get("mode"); mode != "" {
		if mode != "latest" {
			return req, fmt.Errorf("%w: unsupported message page mode %q", store.ErrInvalidMessagePage, mode)
		}
		if cursorCount > 0 {
			return req, fmt.Errorf("%w: mode and cursor params are mutually exclusive", store.ErrInvalidMessagePage)
		}
	}
	return req, nil
}

func writeMessagePage(w http.ResponseWriter, page store.MessagePage, err error) {
	writeResult(w, map[string]any{
		"messages":   page.Messages,
		"oldest_seq": page.OldestSeq,
		"newest_seq": page.NewestSeq,
		"has_older":  page.HasOlder,
		"has_newer":  page.HasNewer,
	}, err)
}
