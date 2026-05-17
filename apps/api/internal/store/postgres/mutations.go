package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) UpdateChannel(ctx context.Context, input store.UpdateChannelInput) (store.Channel, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Channel{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	chRow, err := qtx.GetChannel(ctx, input.ChannelID)
	if err != nil {
		return store.Channel{}, store.Event{}, err
	}
	ch := storeChannelFromGetChannel(chRow)
	if err := requireMembershipTx(ctx, tx, ch.WorkspaceID, input.UserID); err != nil {
		return store.Channel{}, store.Event{}, err
	}
	name := slug(input.Name)
	if name == "" {
		name = ch.Name
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = ch.Kind
	}
	archivedValue := ch.ArchivedAt
	if input.Archived != nil {
		archivedValue = nil
		if *input.Archived {
			value := now()
			archivedValue = &value
		}
	}
	if err := qtx.UpdateChannel(ctx, storedb.UpdateChannelParams{
		Name:       name,
		Kind:       kind,
		ArchivedAt: nullFromPtr(archivedValue),
		ID:         ch.ID,
	}); err != nil {
		return store.Channel{}, store.Event{}, err
	}
	event, err := insertEvent(ctx, tx, ch.WorkspaceID, ch.ID, "channel.updated", nil, map[string]string{"channel_id": ch.ID})
	if err != nil {
		return store.Channel{}, store.Event{}, err
	}
	ch.Name = name
	ch.Kind = kind
	ch.ArchivedAt = archivedValue
	return ch, event, tx.Commit()
}

func (s *Store) UpdateMessage(ctx context.Context, input store.UpdateMessageInput) (store.Message, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	defer tx.Rollback()
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, input.UserID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if msg.AuthorID != input.UserID {
		return store.Message{}, store.Event{}, errors.New("only the author can edit a message")
	}
	if msg.DeletedAt != nil {
		return store.Message{}, store.Event{}, errors.New("deleted messages cannot be edited")
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return store.Message{}, store.Event{}, errors.New("message body is required")
	}
	editedAt := now()
	if err := s.q.WithTx(tx).UpdateMessageBody(ctx, storedb.UpdateMessageBodyParams{Body: body, EditedAt: sqlText(editedAt), ID: msg.ID}); err != nil {
		return store.Message{}, store.Event{}, err
	}
	payload := messagePayload(msg)
	recipients, err := eventRecipientsForMessageTx(ctx, tx, msg)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, msg.WorkspaceID, msg.ChannelID, "message.updated", msg.ChannelSeq, payload, recipients)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	msg.Body = body
	msg.EditedAt = &editedAt
	return msg, event, tx.Commit()
}

func (s *Store) DeleteMessage(ctx context.Context, input store.DeleteMessageInput) (store.Message, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	defer tx.Rollback()
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, input.UserID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if msg.AuthorID != input.UserID {
		return store.Message{}, store.Event{}, errors.New("only the author can delete a message")
	}
	deletedAt := now()
	if err := s.q.WithTx(tx).DeleteMessageBody(ctx, storedb.DeleteMessageBodyParams{DeletedAt: sqlText(deletedAt), ID: msg.ID}); err != nil {
		return store.Message{}, store.Event{}, err
	}
	recipients, err := eventRecipientsForMessageTx(ctx, tx, msg)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, msg.WorkspaceID, msg.ChannelID, "message.deleted", msg.ChannelSeq, messagePayload(msg), recipients)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	msg.Body = ""
	msg.DeletedAt = &deletedAt
	return msg, event, tx.Commit()
}

func messagePayload(msg store.Message) map[string]string {
	payload := map[string]string{"message_id": msg.ID, "root_message_id": msg.ThreadRootID}
	if msg.DirectConversationID != "" {
		payload["direct_conversation_id"] = msg.DirectConversationID
	}
	return payload
}

func eventRecipientsForMessageTx(ctx context.Context, tx *sql.Tx, msg store.Message) ([]string, error) {
	if msg.DirectConversationID == "" {
		return nil, nil
	}
	return directConversationMemberIDsTx(ctx, tx, msg.DirectConversationID)
}
