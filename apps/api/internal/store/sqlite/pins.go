package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) PinMessage(ctx context.Context, channelID, messageID, userID string) (store.PinnedMessage, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	defer tx.Rollback()

	// Resolve message to get workspace + channel context
	msg, err := getMessageTx(ctx, tx, messageID)
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, userID); err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, msg.WorkspaceID, userID); err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}

	// These are ordinary validation errors: the HTTP store-error boundary maps
	// untyped validation failures to 400 (and has request-level coverage).
	// Messages can only be pinned in their own channel
	if msg.ChannelID == "" || msg.ChannelID != channelID {
		return store.PinnedMessage{}, store.Event{}, errors.New("message is not in this channel")
	}
	if msg.DeletedAt != nil {
		return store.PinnedMessage{}, store.Event{}, errors.New("deleted messages cannot be pinned")
	}

	qtx := s.q.WithTx(tx)
	nowTime := now()
	pinID := newID("pin")

	// The count predicate and insert are one SQLite write statement, so two
	// concurrent requests cannot both observe the final available slot.
	affected, err := qtx.PinMessageWithinLimit(ctx, storedb.PinMessageWithinLimitParams{
		ID:          pinID,
		WorkspaceID: msg.WorkspaceID,
		ChannelID:   channelID,
		MessageID:   messageID,
		PinnedBy:    userID,
		CreatedAt:   nowTime,
	})
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	if affected == 0 {
		existingPinCount, countErr := qtx.CountPinnedMessage(ctx, storedb.CountPinnedMessageParams{
			ChannelID: channelID,
			MessageID: messageID,
		})
		if countErr != nil {
			return store.PinnedMessage{}, store.Event{}, countErr
		}
		if existingPinCount > 0 {
			return store.PinnedMessage{}, store.Event{}, store.ErrAlreadyPinned
		}
		return store.PinnedMessage{}, store.Event{}, store.ErrPinnedMessageLimit
	}

	pin := store.PinnedMessage{
		ID:          pinID,
		WorkspaceID: msg.WorkspaceID,
		ChannelID:   channelID,
		MessageID:   messageID,
		PinnedBy:    userID,
		CreatedAt:   nowTime,
	}

	event, err := insertEvent(ctx, tx, msg.WorkspaceID, channelID, "pin.added", msg.ChannelSeq, map[string]string{
		"channel_id": channelID,
		"message_id": messageID,
		"pinned_by":  userID,
	})
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}

	return pin, event, tx.Commit()
}

func (s *Store) UnpinMessage(ctx context.Context, channelID, messageID, userID string) (store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Event{}, err
	}
	defer tx.Rollback()

	// Resolve message to get workspace + channel context
	msg, err := getMessageTx(ctx, tx, messageID)
	if err != nil {
		return store.Event{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, userID); err != nil {
		return store.Event{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, msg.WorkspaceID, userID); err != nil {
		return store.Event{}, err
	}

	// Messages can only be unpinned from their own channel
	if msg.ChannelID == "" || msg.ChannelID != channelID {
		return store.Event{}, errors.New("message is not in this channel")
	}

	qtx := s.q.WithTx(tx)
	affected, err := qtx.UnpinMessage(ctx, storedb.UnpinMessageParams{
		ChannelID: channelID,
		MessageID: messageID,
	})
	if err != nil {
		return store.Event{}, err
	}
	if affected == 0 {
		return store.Event{}, store.ErrPinnedMessageNotFound
	}

	event, err := insertEvent(ctx, tx, msg.WorkspaceID, channelID, "pin.removed", msg.ChannelSeq, map[string]string{
		"channel_id": channelID,
		"message_id": messageID,
		"pinned_by":  userID,
	})
	if err != nil {
		return store.Event{}, err
	}

	return event, tx.Commit()
}

func (s *Store) ListPinnedMessages(ctx context.Context, channelID, userID string, limit int) ([]store.Message, error) {
	if limit <= 0 || limit > store.MaxPinnedMessagesPerChannel {
		limit = store.MaxPinnedMessagesPerChannel
	}

	// Verify user has access to this channel
	ch, err := s.GetChannel(ctx, channelID, userID)
	if err != nil {
		return nil, err
	}

	rows, err := s.q.ListPinnedMessages(ctx, storedb.ListPinnedMessagesParams{
		WorkspaceID: ch.WorkspaceID,
		ChannelID:   channelID,
		LimitCount:  int64(limit),
	})
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil
	}

	// Fetch each message by ID
	msgs := make([]store.Message, 0, len(rows))
	for _, row := range rows {
		msg, err := getMessage(ctx, s.db, row.MessageID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // skip deleted messages
			}
			return nil, err
		}
		if msg.DeletedAt != nil {
			continue
		}
		msgs = append(msgs, msg)
	}

	msgs, err = s.hydrateAttachments(ctx, msgs)
	if err != nil {
		return nil, err
	}

	msgs, err = s.hydrateQuestions(ctx, msgs)
	if err != nil {
		return nil, err
	}

	msgs, err = s.hydrateReactions(ctx, userID, msgs)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}
