package postgres

import (
	"context"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) PinMessage(ctx context.Context, channelID, messageID, userID string) (store.PinnedMessage, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockMessageForPinning(ctx, messageID); err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
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
	if msg.ChannelID == "" || msg.ChannelID != channelID {
		return store.PinnedMessage{}, store.Event{}, errors.New("message is not in this channel")
	}
	if msg.DeletedAt != nil {
		return store.PinnedMessage{}, store.Event{}, errors.New("deleted messages cannot be pinned")
	}

	if _, err := qtx.LockChannelForUpdate(ctx, channelID); err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	existingPinCount, err := qtx.CountPinnedMessage(ctx, storedb.CountPinnedMessageParams{
		ChannelID: channelID,
		MessageID: messageID,
	})
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	if existingPinCount > 0 {
		return store.PinnedMessage{}, store.Event{}, store.ErrAlreadyPinned
	}
	pinCount, err := qtx.CountPinnedMessages(ctx, channelID)
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	if pinCount >= store.MaxPinnedMessagesPerChannel {
		return store.PinnedMessage{}, store.Event{}, store.ErrPinnedMessageLimit
	}
	createdAt := now()
	pinID := newID("pin")
	affected, err := qtx.PinMessage(ctx, storedb.PinMessageParams{
		ID:          pinID,
		WorkspaceID: msg.WorkspaceID,
		ChannelID:   channelID,
		MessageID:   messageID,
		PinnedBy:    userID,
		CreatedAt:   createdAt,
	})
	if err != nil {
		return store.PinnedMessage{}, store.Event{}, err
	}
	if affected == 0 {
		return store.PinnedMessage{}, store.Event{}, store.ErrAlreadyPinned
	}

	pin := store.PinnedMessage{
		ID:          pinID,
		WorkspaceID: msg.WorkspaceID,
		ChannelID:   channelID,
		MessageID:   messageID,
		PinnedBy:    userID,
		CreatedAt:   createdAt,
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
	if msg.ChannelID == "" || msg.ChannelID != channelID {
		return store.Event{}, errors.New("message is not in this channel")
	}

	affected, err := s.q.WithTx(tx).UnpinMessage(ctx, storedb.UnpinMessageParams{
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
	channel, err := s.GetChannel(ctx, channelID, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPinnedMessages(ctx, storedb.ListPinnedMessagesParams{
		WorkspaceID: channel.WorkspaceID,
		ChannelID:   channelID,
		LimitCount:  int32(limit),
	})
	if err != nil {
		return nil, err
	}
	messages := make([]store.Message, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, pinnedMessageFromDB(row))
	}
	messages, err = s.hydrateAttachments(ctx, messages)
	if err != nil {
		return nil, err
	}
	messages, err = s.hydrateReactions(ctx, userID, messages)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func pinnedMessageFromDB(row storedb.ListPinnedMessagesRow) store.Message {
	message := store.Message{
		ID: row.ID, RouteID: row.RouteID, WorkspaceID: row.WorkspaceID,
		ChannelID: row.ChannelID, DirectConversationID: row.DirectConversationID,
		AuthorID: row.AuthorID, ThreadRootID: row.ThreadRootID, TopicID: row.TopicID,
		Body: row.Body, BodyFormat: row.BodyFormat, CreatedAt: row.CreatedAt,
		QuotedBodySnapshot: row.QuotedBodySnapshot, Nonce: row.ClientNonce,
		Kind: row.MessageKind, TurnID: row.TurnID,
		Author: &store.User{
			ID: row.UserID, Kind: row.UserKind, DisplayName: row.DisplayName,
			Handle: row.Handle, AvatarURL: row.AvatarUrl, CreatedAt: row.UserCreatedAt,
		},
	}
	if row.ParentMessageID.Valid {
		message.ParentMessageID = &row.ParentMessageID.String
	}
	if row.ChannelSeq.Valid {
		message.ChannelSeq = &row.ChannelSeq.Int64
	}
	if row.ThreadSeq.Valid {
		message.ThreadSeq = &row.ThreadSeq.Int64
	}
	if row.EditedAt.Valid {
		message.EditedAt = &row.EditedAt.String
	}
	if row.DeletedAt.Valid {
		message.DeletedAt = &row.DeletedAt.String
	}
	if row.OwnerUserID.Valid {
		message.Author.OwnerUserID = row.OwnerUserID.String
	}
	if row.AuthorFormerHandle.Valid {
		message.Author.FormerHandle = row.AuthorFormerHandle.String
	}
	if row.AuthorDeletedAt.Valid {
		message.Author.DeletedAt = &row.AuthorDeletedAt.String
	}
	if row.QuotedMessageID.Valid {
		message.QuotedMessageID = &row.QuotedMessageID.String
	}
	if row.QuotedAuthorID.Valid {
		message.QuotedAuthorID = &row.QuotedAuthorID.String
	}
	if row.QuotedUserID.Valid {
		message.QuotedAuthor = &store.User{
			ID: row.QuotedUserID.String, Kind: row.QuotedUserKind.String,
			OwnerUserID: row.QuotedOwnerUserID.String, DisplayName: row.QuotedDisplayName.String,
			Handle: row.QuotedHandle.String, AvatarURL: row.QuotedAvatarUrl.String,
			CreatedAt: row.QuotedUserCreatedAt.String,
		}
		if row.QuotedFormerHandle.Valid {
			message.QuotedAuthor.FormerHandle = row.QuotedFormerHandle.String
		}
		if row.QuotedDeletedAt.Valid {
			message.QuotedAuthor.DeletedAt = &row.QuotedDeletedAt.String
		}
	}
	return message
}
