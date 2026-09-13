package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// requireQuestionAuthor admits questions from bot tokens only; people answer them.
// Agent activity rows fold into progress blocks, so they cannot carry one.
func requireQuestionAuthor(w http.ResponseWriter, act actor, question *store.QuestionSpec, kind string) bool {
	if question == nil {
		return true
	}
	if act.botTokenID == "" {
		writeError(w, http.StatusForbidden, errors.New("questions require a bot token"))
		return false
	}
	if store.IsActivityMessageKind(kind) {
		writeError(w, http.StatusBadRequest, errors.New("questions attach only to ordinary messages"))
		return false
	}
	return true
}

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(store.QuestionCapabilityHeader, store.QuestionCapabilityHeaderValue)
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot answer questions"))
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Answers         map[string][]string `json:"answers"`
		Skip            bool                `json:"skip"`
		Nonce           string              `json:"nonce"`
		ExpectedVersion *int64              `json:"expected_version"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Skip && len(body.Answers) > 0 {
		writeError(w, http.StatusBadRequest, errors.New("send answers or skip, not both"))
		return
	}
	message, events, err := s.store.AnswerQuestion(r.Context(), store.AnswerQuestionInput{
		MessageID:       chi.URLParam(r, "message_id"),
		UserID:          act.user.ID,
		Answers:         body.Answers,
		Skip:            body.Skip,
		Nonce:           body.Nonce,
		ExpectedVersion: body.ExpectedVersion,
	})
	if err != nil {
		writeQuestionError(w, err)
		return
	}
	s.publishEvents(r.Context(), events)
	writeJSON(w, http.StatusOK, map[string]any{"message": message, "events": nonNilEvents(events)})
}

func (s *Server) resolveQuestion(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID == "" {
		writeError(w, http.StatusForbidden, errors.New("only the bot that asked a question can resolve it"))
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Status          string              `json:"status"`
		Note            string              `json:"note"`
		Answers         map[string][]string `json:"answers"`
		ExpectedVersion *int64              `json:"expected_version"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireBotMessageResource(w, r, act, chi.URLParam(r, "message_id"), "dms:write"); !ok {
		return
	}
	message, event, err := s.store.ResolveQuestion(r.Context(), store.ResolveQuestionInput{
		MessageID:       chi.URLParam(r, "message_id"),
		BotUserID:       act.user.ID,
		Status:          body.Status,
		Note:            body.Note,
		Answers:         body.Answers,
		ExpectedVersion: body.ExpectedVersion,
	})
	if err != nil {
		writeQuestionError(w, err)
		return
	}
	response := map[string]any{"message": message}
	if event.ID != "" {
		s.publishEvent(r.Context(), event)
		response["event"] = event
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listBotQuestions(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID == "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens only"))
		return
	}
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > store.MaxBotQuestionPageSize {
			writeError(w, http.StatusBadRequest, fmt.Errorf("limit must be between 1 and %d", store.MaxBotQuestionPageSize))
			return
		}
		limit = parsed
	}
	// Direct-message questions stay behind the same scope as direct messages.
	includeDirect := act.requireScope("dms:read") == nil
	questions, err := s.store.ListBotUnresolvedQuestions(r.Context(), act.workspaceID, act.user.ID, strings.TrimSpace(r.URL.Query().Get("after")), includeDirect, limit+1)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if len(questions) > limit {
		questions = questions[:limit]
		cursor := questions[limit-1].MessageID
		next = &cursor
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": questions, "next_cursor": next})
}

func writeQuestionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, errors.New("question not found"))
	case errors.Is(err, store.ErrQuestionClosed), errors.Is(err, store.ErrQuestionConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrQuestionResponderRequired), errors.Is(err, store.ErrQuestionAuthorRequired):
		writeError(w, http.StatusForbidden, err)
	default:
		writeStoreError(w, err)
	}
}

func nonNilEvents(events []store.Event) []store.Event {
	if events == nil {
		return []store.Event{}
	}
	return events
}
