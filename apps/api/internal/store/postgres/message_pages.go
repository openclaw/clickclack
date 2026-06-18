package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

type messagePageScope struct {
	where string
	args  []any
}

type messagePageMode string

const (
	messagePageLatest messagePageMode = "latest"
	messagePageBefore messagePageMode = "before"
	messagePageAfter  messagePageMode = "after"
	messagePageAround messagePageMode = "around"
)

func normalizeMessagePageRequest(req store.MessagePageRequest) (store.MessagePageRequest, messagePageMode, error) {
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 100
	}
	cursorCount := 0
	mode := messagePageLatest
	for _, cursor := range []struct {
		value **int64
		mode  messagePageMode
	}{
		{&req.BeforeSeq, messagePageBefore},
		{&req.AfterSeq, messagePageAfter},
		{&req.AroundSeq, messagePageAround},
	} {
		if *cursor.value == nil {
			continue
		}
		if **cursor.value < 0 {
			return req, "", fmt.Errorf("%w: cursor must be non-negative", store.ErrInvalidMessagePage)
		}
		cursorCount++
		mode = cursor.mode
	}
	if cursorCount > 1 {
		return req, "", fmt.Errorf("%w: before_seq, after_seq, and around_seq are mutually exclusive", store.ErrInvalidMessagePage)
	}
	return req, mode, nil
}

func (s *Store) listMessagePage(ctx context.Context, scope messagePageScope, req store.MessagePageRequest) (store.MessagePage, error) {
	req, mode, err := normalizeMessagePageRequest(req)
	if err != nil {
		return store.MessagePage{}, err
	}

	var messages []store.Message
	switch mode {
	case messagePageLatest:
		messages, err = s.queryScopedMessages(ctx, scope, "", nil, "DESC", req.Limit)
		reverseMessages(messages)
	case messagePageBefore:
		messages, err = s.queryScopedMessages(ctx, scope, "m.channel_seq < $%d", []any{*req.BeforeSeq}, "DESC", req.Limit)
		reverseMessages(messages)
	case messagePageAfter:
		messages, err = s.queryScopedMessages(ctx, scope, "m.channel_seq > $%d", []any{*req.AfterSeq}, "ASC", req.Limit)
	case messagePageAround:
		messages, err = s.queryMessagesAround(ctx, scope, *req.AroundSeq, req.Limit)
	default:
		err = errors.New("unknown message page mode")
	}
	if err != nil {
		return store.MessagePage{}, err
	}
	messages, err = s.hydrateAttachments(ctx, messages)
	if err != nil {
		return store.MessagePage{}, err
	}
	messages, err = s.hydrateThreadStates(ctx, messages)
	if err != nil {
		return store.MessagePage{}, err
	}
	return s.buildMessagePage(ctx, scope, messages)
}

func (s *Store) hydrateThreadStates(ctx context.Context, messages []store.Message) ([]store.Message, error) {
	rootIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.ParentMessageID == nil {
			rootIDs = append(rootIDs, message.ID)
		}
	}
	if len(rootIDs) == 0 {
		return messages, nil
	}
	rows, err := storedb.New(s.db).ListThreadStates(ctx, rootIDs)
	if err != nil {
		return nil, err
	}
	states := make(map[string]store.ThreadState, len(rootIDs))
	for _, row := range rows {
		states[row.RootMessageID] = storeThreadStateFromDB(row)
	}
	for i := range messages {
		if messages[i].ParentMessageID != nil {
			continue
		}
		state, ok := states[messages[i].ID]
		if !ok {
			state = store.ThreadState{RootMessageID: messages[i].ID}
		}
		stateCopy := state
		messages[i].ThreadState = &stateCopy
	}
	return messages, nil
}

func (s *Store) queryScopedMessages(ctx context.Context, scope messagePageScope, cursorWhere string, cursorArgs []any, order string, limit int) ([]store.Message, error) {
	where := scope.where
	args := append([]any{}, scope.args...)
	if cursorWhere != "" {
		where += " AND " + fmt.Sprintf(cursorWhere, len(args)+1)
		args = append(args, cursorArgs...)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, messageSelect()+`
		WHERE `+where+`
		ORDER BY m.channel_seq `+order+`
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) queryMessagesAround(ctx context.Context, scope messagePageScope, cursor int64, limit int) ([]store.Message, error) {
	leftLimit := limit/2 + limit%2
	rightLimit := limit - leftLimit

	left, err := s.queryScopedMessages(ctx, scope, "m.channel_seq <= $%d", []any{cursor}, "DESC", limit)
	if err != nil {
		return nil, err
	}
	takeLeft := min(len(left), leftLimit)
	right, err := s.queryScopedMessages(ctx, scope, "m.channel_seq > $%d", []any{cursor}, "ASC", rightLimit+(leftLimit-takeLeft))
	if err != nil {
		return nil, err
	}
	if len(right) < rightLimit+(leftLimit-takeLeft) {
		takeLeft = min(len(left), limit-len(right))
	}
	left = append([]store.Message{}, left[:takeLeft]...)
	reverseMessages(left)
	return append(left, right...), nil
}

func (s *Store) buildMessagePage(ctx context.Context, scope messagePageScope, messages []store.Message) (store.MessagePage, error) {
	page := store.MessagePage{Messages: messages}
	if len(messages) == 0 {
		return page, nil
	}
	page.OldestSeq = messageSeq(messages[0])
	page.NewestSeq = messageSeq(messages[len(messages)-1])
	hasOlder, err := s.hasScopedMessage(ctx, scope, "m.channel_seq < $%d", page.OldestSeq)
	if err != nil {
		return store.MessagePage{}, err
	}
	hasNewer, err := s.hasScopedMessage(ctx, scope, "m.channel_seq > $%d", page.NewestSeq)
	if err != nil {
		return store.MessagePage{}, err
	}
	page.HasOlder = hasOlder
	page.HasNewer = hasNewer
	return page, nil
}

func (s *Store) hasScopedMessage(ctx context.Context, scope messagePageScope, cursorWhere string, cursor int64) (bool, error) {
	args := append([]any{}, scope.args...)
	args = append(args, cursor)
	rows, err := s.db.QueryContext(ctx, `
		SELECT 1
		FROM messages m
		WHERE `+scope.where+` AND `+fmt.Sprintf(cursorWhere, len(args))+`
		LIMIT 1`, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func messageSeq(message store.Message) int64 {
	if message.ChannelSeq == nil {
		return 0
	}
	return *message.ChannelSeq
}

func reverseMessages(messages []store.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}
