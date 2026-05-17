package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

// MarkChannelRead upserts the user's read pointer for a channel and emits a
// `channel.read` event. The pointer is monotonic — calls with a smaller seq
// are no-ops and return the existing receipt.
func (s *Store) MarkChannelRead(ctx context.Context, channelID, userID string, seq int64) (store.ReadReceipt, store.Event, error) {
	if seq < 0 {
		return store.ReadReceipt{}, store.Event{}, errors.New("seq must be non-negative")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	workspaceID, err := qtx.GetChannelWorkspace(ctx, channelID)
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, userID); err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	// Cap to the current channel last_seq so a buggy client can't push the
	// pointer beyond reality.
	lastSeq, err := qtx.ChannelLastSeq(ctx, channelID)
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	if seq > lastSeq {
		seq = lastSeq
	}
	current, currentAt, err := readChannelRead(ctx, qtx, channelID, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.ReadReceipt{}, store.Event{}, err
	}
	if seq <= current {
		return store.ReadReceipt{ScopeID: channelID, UserID: userID, LastReadSeq: current, LastReadAt: currentAt}, store.Event{}, nil
	}
	at := now()
	if err := qtx.UpsertChannelRead(ctx, storedb.UpsertChannelReadParams{
		ChannelID:   channelID,
		UserID:      userID,
		LastReadSeq: seq,
		LastReadAt:  at,
	}); err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, workspaceID, channelID, "channel.read", &seq, map[string]string{
		"channel_id": channelID,
		"user_id":    userID,
	}, []string{userID})
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	return store.ReadReceipt{ScopeID: channelID, UserID: userID, LastReadSeq: seq, LastReadAt: at}, event, tx.Commit()
}

// MarkDirectRead is the DM analogue of MarkChannelRead.
func (s *Store) MarkDirectRead(ctx context.Context, conversationID, userID string, seq int64) (store.ReadReceipt, store.Event, error) {
	if seq < 0 {
		return store.ReadReceipt{}, store.Event{}, errors.New("seq must be non-negative")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	workspaceID, err := qtx.GetDirectConversationWorkspace(ctx, conversationID)
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	if err := requireDirectMembershipTx(ctx, tx, conversationID, userID); err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	lastSeq, err := qtx.DirectLastSeq(ctx, conversationID)
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	if seq > lastSeq {
		seq = lastSeq
	}
	current, currentAt, err := readDirectRead(ctx, qtx, conversationID, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.ReadReceipt{}, store.Event{}, err
	}
	if seq <= current {
		return store.ReadReceipt{ScopeID: conversationID, UserID: userID, LastReadSeq: current, LastReadAt: currentAt}, store.Event{}, nil
	}
	at := now()
	if err := qtx.UpsertDirectRead(ctx, storedb.UpsertDirectReadParams{
		ConversationID: conversationID,
		UserID:         userID,
		LastReadSeq:    seq,
		LastReadAt:     at,
	}); err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, workspaceID, "", "dm.read", &seq, map[string]string{
		"direct_conversation_id": conversationID,
		"user_id":                userID,
	}, []string{userID})
	if err != nil {
		return store.ReadReceipt{}, store.Event{}, err
	}
	return store.ReadReceipt{ScopeID: conversationID, UserID: userID, LastReadSeq: seq, LastReadAt: at}, event, tx.Commit()
}

func readChannelRead(ctx context.Context, q *storedb.Queries, channelID, userID string) (int64, string, error) {
	row, err := q.ReadChannelRead(ctx, storedb.ReadChannelReadParams{ChannelID: channelID, UserID: userID})
	return row.LastReadSeq, row.LastReadAt, err
}

func readDirectRead(ctx context.Context, q *storedb.Queries, conversationID, userID string) (int64, string, error) {
	row, err := q.ReadDirectRead(ctx, storedb.ReadDirectReadParams{ConversationID: conversationID, UserID: userID})
	return row.LastReadSeq, row.LastReadAt, err
}
