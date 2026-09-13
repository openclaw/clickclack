package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

// questionDocument is the immutable part of a question stored in spec_json.
type questionDocument struct {
	Title string               `json:"title,omitempty"`
	Items []store.QuestionItem `json:"items"`
}

type storedQuestionResponse struct {
	Answers map[string][]string `json:"answers,omitempty"`
	Skipped bool                `json:"skipped,omitempty"`
}

// prepareQuestion normalizes a bot's question. It reads nothing, so a nonce
// replay can compare it before any rule that depends on the current time or
// membership.
func prepareQuestion(input *store.QuestionSpec) (*store.QuestionSpec, error) {
	if input == nil {
		return nil, nil
	}
	spec, err := store.NormalizeQuestionSpec(*input)
	if err != nil {
		return nil, err
	}
	return &spec, nil
}

// validateNewQuestionTx checks what a newly created question must satisfy now:
// an ordinary message, its deadline, and responders who can read the conversation.
func validateNewQuestionTx(ctx context.Context, tx *sql.Tx, workspaceID, channelID, directConversationID, kind string, spec *store.QuestionSpec) error {
	if spec == nil {
		return nil
	}
	if store.IsActivityMessageKind(kind) {
		return fmt.Errorf("%w: %s", store.ErrInvalidQuestion, "questions attach only to ordinary messages")
	}
	if err := store.ValidateQuestionLifetime(*spec, time.Now()); err != nil {
		return err
	}
	for _, userID := range spec.ResponderUserIDs {
		var one int
		query := `
			SELECT 1
			FROM workspace_members wm
			JOIN users u ON u.id = wm.user_id
			WHERE wm.workspace_id = $1 AND wm.user_id = $2 AND u.kind = 'human'`
		args := []any{workspaceID, userID}
		if directConversationID != "" {
			query += ` AND EXISTS (SELECT 1 FROM direct_conversation_members dcm WHERE dcm.conversation_id = $3 AND dcm.user_id = wm.user_id)`
			args = append(args, directConversationID)
		}
		err := tx.QueryRowContext(ctx, query, args...).Scan(&one)
		if err == nil && directConversationID == "" {
			err = requireGuestChannelAccessTx(ctx, tx, workspaceID, channelID, userID)
		}
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrModerationRestricted) {
			return fmt.Errorf("%w: %s", store.ErrInvalidQuestion, "responder_user_ids must be people who can read this conversation")
		} else if err != nil {
			return err
		}
	}
	return nil
}

func insertMessageQuestionTx(ctx context.Context, tx *sql.Tx, messageID, workspaceID, botUserID, createdAt string, spec *store.QuestionSpec) (*store.MessageQuestion, error) {
	if spec == nil {
		return nil, nil
	}
	document, err := json.Marshal(questionDocument{Title: spec.Title, Items: spec.Items})
	if err != nil {
		return nil, err
	}
	responders, err := json.Marshal(nonNilStrings(spec.ResponderUserIDs))
	if err != nil {
		return nil, err
	}
	allowSkip := int64(0)
	if spec.AllowSkip == nil || *spec.AllowSkip {
		allowSkip = 1
	}
	if err := storedb.New(tx).InsertMessageQuestion(ctx, storedb.InsertMessageQuestionParams{
		MessageID:        messageID,
		WorkspaceID:      workspaceID,
		BotUserID:        botUserID,
		ExternalID:       spec.ExternalID,
		SpecJson:         string(document),
		ResponderUserIds: string(responders),
		AllowSkip:        allowSkip,
		ExpiresAt:        spec.ExpiresAt,
		CreatedAt:        createdAt,
	}); err != nil {
		return nil, err
	}
	return &store.MessageQuestion{
		Status:           store.QuestionStatusOpen,
		ExternalID:       spec.ExternalID,
		Title:            spec.Title,
		ExpiresAt:        spec.ExpiresAt,
		AllowSkip:        allowSkip == 1,
		Items:            spec.Items,
		ResponderUserIDs: spec.ResponderUserIDs,
		Version:          1,
	}, nil
}

// questionReplayMatchesTx reports whether a nonce replay repeats the same question.
func questionReplayMatchesTx(ctx context.Context, tx *sql.Tx, messageID string, spec *store.QuestionSpec) (bool, error) {
	row, err := storedb.New(tx).GetMessageQuestion(ctx, messageID)
	if errors.Is(err, sql.ErrNoRows) {
		return spec == nil, nil
	}
	if err != nil || spec == nil {
		return false, err
	}
	document, err := json.Marshal(questionDocument{Title: spec.Title, Items: spec.Items})
	if err != nil {
		return false, err
	}
	var responders []string
	if err := json.Unmarshal([]byte(row.ResponderUserIds), &responders); err != nil {
		return false, err
	}
	allowSkip := spec.AllowSkip == nil || *spec.AllowSkip
	return row.SpecJson == string(document) && row.ExternalID == spec.ExternalID && row.ExpiresAt == spec.ExpiresAt &&
		(row.AllowSkip == 1) == allowSkip && slices.Equal(responders, nonNilStrings(spec.ResponderUserIDs)), nil
}

func (s *Store) hydrateQuestions(ctx context.Context, messages []store.Message) ([]store.Message, error) {
	return hydrateQuestions(ctx, s.db, messages)
}

func hydrateQuestions(ctx context.Context, db storedb.DBTX, messages []store.Message) ([]store.Message, error) {
	indexByID := make(map[string]int, len(messages))
	args := make([]any, 0, len(messages))
	for index, message := range messages {
		if message.DeletedAt != nil {
			continue
		}
		indexByID[message.ID] = index
		args = append(args, message.ID)
	}
	if len(args) == 0 {
		return messages, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT q.message_id, q.external_id, q.spec_json, q.responder_user_ids, q.allow_skip, q.expires_at, q.status,
		       q.response_json, q.response_source, q.responded_at, q.note, q.resolved_at, q.version,
		       u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
		FROM message_questions q
		LEFT JOIN users u ON u.id = q.responded_by
		WHERE q.message_id IN (`+pgPlaceholders(len(args), 1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var row storedb.MessageQuestion
		var responderID, responderKind, responderOwner, responderName, responderHandle, responderAvatar, responderCreated sql.NullString
		if err := rows.Scan(&row.MessageID, &row.ExternalID, &row.SpecJson, &row.ResponderUserIds, &row.AllowSkip, &row.ExpiresAt, &row.Status,
			&row.ResponseJson, &row.ResponseSource, &row.RespondedAt, &row.Note, &row.ResolvedAt, &row.Version,
			&responderID, &responderKind, &responderOwner, &responderName, &responderHandle, &responderAvatar, &responderCreated); err != nil {
			return nil, err
		}
		var responder *store.User
		if responderID.Valid {
			user := storeUserFromDB(responderID.String, responderKind.String, responderOwner, responderName.String, responderHandle.String, responderAvatar.String, responderCreated.String)
			responder = &user
		}
		question, err := messageQuestionFromRow(row, responder, now)
		if err != nil {
			return nil, err
		}
		if index, ok := indexByID[row.MessageID]; ok {
			messages[index].Question = &question
		}
	}
	return messages, rows.Err()
}

func messageQuestionFromRow(row storedb.MessageQuestion, responder *store.User, now time.Time) (store.MessageQuestion, error) {
	var document questionDocument
	if err := json.Unmarshal([]byte(row.SpecJson), &document); err != nil {
		return store.MessageQuestion{}, err
	}
	question := store.MessageQuestion{
		Status:     store.EffectiveQuestionStatus(row.Status, row.ExpiresAt, now),
		ExternalID: row.ExternalID,
		Title:      document.Title,
		ExpiresAt:  row.ExpiresAt,
		AllowSkip:  row.AllowSkip == 1,
		Items:      document.Items,
		Note:       row.Note,
		ResolvedAt: ptrFromNull(row.ResolvedAt),
		Version:    row.Version,
	}
	if err := json.Unmarshal([]byte(row.ResponderUserIds), &question.ResponderUserIDs); err != nil {
		return store.MessageQuestion{}, err
	}
	if len(question.ResponderUserIDs) == 0 {
		question.ResponderUserIDs = nil
	}
	if row.ResponseJson != "" {
		var response storedQuestionResponse
		if err := json.Unmarshal([]byte(row.ResponseJson), &response); err != nil {
			return store.MessageQuestion{}, err
		}
		question.Response = &store.QuestionResponse{
			Answers:     response.Answers,
			Skipped:     response.Skipped,
			Source:      row.ResponseSource,
			Responder:   responder,
			RespondedAt: row.RespondedAt.String,
		}
	}
	return question, nil
}

func (s *Store) AnswerQuestion(ctx context.Context, input store.AnswerQuestionInput) (store.Message, []store.Event, error) {
	nonce, err := store.NormalizeClientNonce(input.Nonce)
	if err != nil {
		return store.Message{}, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, nil, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return store.Message{}, nil, err
	}
	if msg.DeletedAt != nil {
		return store.Message{}, nil, sql.ErrNoRows
	}
	row, err := qtx.GetMessageQuestion(ctx, msg.ID)
	if err != nil {
		return store.Message{}, nil, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, input.UserID); err != nil {
		return store.Message{}, nil, err
	}
	if msg.DirectConversationID != "" {
		err = requireCanSendDirectTx(ctx, tx, msg.WorkspaceID, input.UserID)
	} else {
		err = requireNoModerationBlockTx(ctx, tx, msg.WorkspaceID, input.UserID)
	}
	if err != nil {
		return store.Message{}, nil, err
	}
	var responders []string
	if err := json.Unmarshal([]byte(row.ResponderUserIds), &responders); err != nil {
		return store.Message{}, nil, err
	}
	if len(responders) > 0 && !slices.Contains(responders, input.UserID) {
		return store.Message{}, nil, store.ErrQuestionResponderRequired
	}
	if nonce != "" && row.ResponseNonce == nonce && row.RespondedBy.String == input.UserID {
		// Release the transaction first: reads below use the store's connection pool.
		if err := tx.Rollback(); err != nil {
			return store.Message{}, nil, err
		}
		message, err := s.hydrateQuestionMessage(ctx, msg, input.UserID)
		return message, nil, err
	}
	if input.ExpectedVersion != nil && *input.ExpectedVersion != row.Version {
		return store.Message{}, nil, store.ErrQuestionConflict
	}
	if store.EffectiveQuestionStatus(row.Status, row.ExpiresAt, time.Now()) != store.QuestionStatusOpen {
		return store.Message{}, nil, store.ErrQuestionClosed
	}
	response := storedQuestionResponse{Skipped: input.Skip}
	if input.Skip {
		if row.AllowSkip != 1 {
			return store.Message{}, nil, fmt.Errorf("%w: %s", store.ErrInvalidQuestion, "this question cannot be skipped")
		}
	} else {
		var document questionDocument
		if err := json.Unmarshal([]byte(row.SpecJson), &document); err != nil {
			return store.Message{}, nil, err
		}
		if response.Answers, err = store.NormalizeQuestionAnswers(document.Items, input.Answers); err != nil {
			return store.Message{}, nil, err
		}
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return store.Message{}, nil, err
	}
	respondedAt := now()
	affected, err := qtx.SubmitMessageQuestionResponse(ctx, storedb.SubmitMessageQuestionResponseParams{
		ResponseJson:  string(responseJSON),
		RespondedBy:   sqlText(input.UserID),
		RespondedAt:   sqlText(respondedAt),
		ResponseNonce: nonce,
		MessageID:     msg.ID,
		Version:       row.Version,
	})
	if err != nil {
		return store.Message{}, nil, err
	}
	if affected == 0 {
		return store.Message{}, nil, store.ErrQuestionClosed
	}
	events, err := insertQuestionEventsTx(ctx, tx, msg, map[string]any{
		"external_id":  row.ExternalID,
		"responder_id": input.UserID,
		"skipped":      input.Skip,
		"version":      row.Version + 1,
	})
	if err != nil {
		return store.Message{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return store.Message{}, nil, err
	}
	message, err := s.hydrateQuestionMessage(ctx, msg, input.UserID)
	return message, events, err
}

func (s *Store) ResolveQuestion(ctx context.Context, input store.ResolveQuestionInput) (store.Message, store.Event, error) {
	status := strings.TrimSpace(input.Status)
	note := strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(note) > store.MaxQuestionNoteLength {
		return store.Message{}, store.Event{}, fmt.Errorf("%w: %s", store.ErrInvalidQuestion, "note is too long")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if msg.DeletedAt != nil {
		return store.Message{}, store.Event{}, sql.ErrNoRows
	}
	row, err := qtx.GetMessageQuestion(ctx, msg.ID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if row.BotUserID != input.BotUserID {
		return store.Message{}, store.Event{}, store.ErrQuestionAuthorRequired
	}
	if input.ExpectedVersion != nil && *input.ExpectedVersion != row.Version {
		return store.Message{}, store.Event{}, store.ErrQuestionConflict
	}
	if row.Status == status && store.IsTerminalQuestionStatus(status) {
		if err := tx.Rollback(); err != nil {
			return store.Message{}, store.Event{}, err
		}
		message, err := s.hydrateQuestionMessage(ctx, msg, input.BotUserID)
		return message, store.Event{}, err
	}
	if !store.QuestionResolutionAllowed(row.Status, status) {
		return store.Message{}, store.Event{}, store.ErrQuestionConflict
	}
	params := storedb.ResolveMessageQuestionParams{
		Status:         status,
		Note:           note,
		ResponseJson:   row.ResponseJson,
		ResponseSource: row.ResponseSource,
		RespondedBy:    row.RespondedBy,
		RespondedAt:    row.RespondedAt,
		ResponseNonce:  row.ResponseNonce,
		UpdatedAt:      now(),
		MessageID:      msg.ID,
		Version:        row.Version,
	}
	switch {
	case status == store.QuestionStatusOpen:
		if note == "" {
			return store.Message{}, store.Event{}, fmt.Errorf("%w: %s", store.ErrInvalidQuestion, "reopening a question requires a note")
		}
		params.ResponseJson, params.ResponseSource, params.ResponseNonce = "", "", ""
		params.RespondedBy, params.RespondedAt = sql.NullString{}, sql.NullString{}
	case row.Status == store.QuestionStatusOpen && status == store.QuestionStatusAnswered && len(input.Answers) > 0:
		var document questionDocument
		if err := json.Unmarshal([]byte(row.SpecJson), &document); err != nil {
			return store.Message{}, store.Event{}, err
		}
		answers, err := store.NormalizeQuestionAnswers(document.Items, input.Answers)
		if err != nil {
			return store.Message{}, store.Event{}, err
		}
		responseJSON, err := json.Marshal(storedQuestionResponse{Answers: answers})
		if err != nil {
			return store.Message{}, store.Event{}, err
		}
		params.ResponseJson = string(responseJSON)
		params.ResponseSource = store.QuestionResponseSourceExternal
		params.RespondedAt = sqlText(params.UpdatedAt)
	}
	if store.IsTerminalQuestionStatus(status) {
		params.ResolvedAt = sqlText(params.UpdatedAt)
	}
	affected, err := qtx.ResolveMessageQuestion(ctx, params)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if affected == 0 {
		return store.Message{}, store.Event{}, store.ErrQuestionConflict
	}
	events, err := insertQuestionEventsTx(ctx, tx, msg, nil)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Message{}, store.Event{}, err
	}
	message, err := s.hydrateQuestionMessage(ctx, msg, input.BotUserID)
	return message, events[0], err
}

// insertQuestionEventsTx appends message.updated so every client refreshes the
// card and, for a submitted answer, question.submitted for the authoring bot.
// Neither payload carries answer content.
func insertQuestionEventsTx(ctx context.Context, tx *sql.Tx, msg store.Message, submitted map[string]any) ([]store.Event, error) {
	recipients, err := eventRecipientsForMessageTx(ctx, tx, msg)
	if err != nil {
		return nil, err
	}
	updated, err := insertEventWithRecipients(ctx, tx, msg.WorkspaceID, msg.ChannelID, "message.updated", msg.ChannelSeq, messagePayload(msg), recipients)
	if err != nil || submitted == nil {
		return []store.Event{updated}, err
	}
	submitted["message_id"] = msg.ID
	submitted["root_message_id"] = msg.ThreadRootID
	if msg.DirectConversationID != "" {
		submitted["direct_conversation_id"] = msg.DirectConversationID
	}
	event, err := insertEventWithRecipients(ctx, tx, msg.WorkspaceID, msg.ChannelID, "question.submitted", msg.ChannelSeq, submitted, recipients)
	if err != nil {
		return nil, err
	}
	return []store.Event{updated, event}, nil
}

func (s *Store) hydrateQuestionMessage(ctx context.Context, msg store.Message, userID string) (store.Message, error) {
	fresh, err := getMessage(ctx, s.db, msg.ID)
	if err != nil {
		return store.Message{}, err
	}
	messages, err := s.hydrateAttachments(ctx, []store.Message{fresh})
	if err != nil {
		return store.Message{}, err
	}
	if messages, err = s.hydrateReactions(ctx, userID, messages); err != nil {
		return store.Message{}, err
	}
	if messages, err = s.hydrateQuestions(ctx, messages); err != nil {
		return store.Message{}, err
	}
	return messages[0], nil
}

func (s *Store) ListBotUnresolvedQuestions(ctx context.Context, workspaceID, botUserID, afterMessageID string, includeDirect bool, limit int) ([]store.BotQuestion, error) {
	// One row past the largest page lets callers detect a next page.
	if limit <= 0 || limit > store.MaxBotQuestionPageSize+1 {
		limit = store.MaxBotQuestionPageSize + 1
	}
	rows, err := s.q.ListBotUnresolvedQuestions(ctx, storedb.ListBotUnresolvedQuestionsParams{
		WorkspaceID:    workspaceID,
		BotUserID:      botUserID,
		AfterMessageID: afterMessageID,
		IncludeDirect:  includeDirect,
		RowLimit:       int32(limit),
	})
	if err != nil {
		return nil, err
	}
	questions := make([]store.BotQuestion, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		questions = append(questions, store.BotQuestion{
			MessageID:            row.MessageID,
			WorkspaceID:          row.WorkspaceID,
			ChannelID:            row.ChannelID,
			DirectConversationID: row.DirectConversationID,
			ThreadRootID:         row.ThreadRootID,
			ExternalID:           row.ExternalID,
			Status:               store.EffectiveQuestionStatus(row.Status, row.ExpiresAt, now),
			ExpiresAt:            row.ExpiresAt,
			Version:              row.Version,
		})
	}
	return questions, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// mergeMentionedUserIDs adds a question's responders to the resolved mentions so
// mention-only channels still alert the people who must answer.
func mergeMentionedUserIDs(mentioned []string, spec *store.QuestionSpec) []string {
	if spec == nil {
		return mentioned
	}
	for _, userID := range spec.ResponderUserIDs {
		if !slices.Contains(mentioned, userID) {
			mentioned = append(mentioned, userID)
		}
	}
	return mentioned
}
