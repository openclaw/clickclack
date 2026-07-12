package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) UpdateWorkspace(ctx context.Context, input store.UpdateWorkspaceInput) (store.Workspace, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	currentRow, err := qtx.GetWorkspace(ctx, storedb.GetWorkspaceParams{WorkspaceID: input.WorkspaceID, UserID: input.ActorUserID})
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	current := storeWorkspaceFromGetWorkspace(currentRow)
	if err := requireNoModerationBlockTx(ctx, tx, input.WorkspaceID, input.ActorUserID); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	if err := requireWorkspaceManagerTx(ctx, tx, input.WorkspaceID, input.ActorUserID); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	name, workspaceSlug, iconURL, err := normalizeWorkspaceSettings(current, input)
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET name = ?, slug = ?, icon_url = ? WHERE id = ?`, name, workspaceSlug, iconURL, input.WorkspaceID); err != nil {
		return store.Workspace{}, store.Event{}, workspaceMutationError(err)
	}
	event, err := insertEvent(ctx, tx, input.WorkspaceID, "", "workspace.updated", nil, map[string]string{"workspace_id": input.WorkspaceID})
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	updatedRow, err := qtx.GetWorkspace(ctx, storedb.GetWorkspaceParams{WorkspaceID: input.WorkspaceID, UserID: input.ActorUserID})
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	return storeWorkspaceFromGetWorkspace(updatedRow), event, tx.Commit()
}

func (s *Store) TransferWorkspaceOwnership(ctx context.Context, input store.TransferWorkspaceOwnershipInput) (store.Workspace, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	defer tx.Rollback()
	if err := requireWorkspaceOwnerTx(ctx, tx, input.WorkspaceID, input.ActorUserID); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	targetID := strings.TrimSpace(input.NewOwnerUserID)
	if targetID == "" || targetID == input.ActorUserID {
		return store.Workspace{}, store.Event{}, errors.New("new owner must be another workspace member")
	}
	var targetRole string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, input.WorkspaceID, targetID).Scan(&targetRole); err != nil {
		return store.Workspace{}, store.Event{}, errors.New("new owner must be a workspace member")
	}
	if targetRole == store.WorkspaceRoleBot || targetRole == store.WorkspaceRoleGuest {
		return store.Workspace{}, store.Event{}, errors.New("new owner must be a human member or moderator")
	}
	qtx := s.q.WithTx(tx)
	if err := qtx.UpdateWorkspaceMemberRole(ctx, storedb.UpdateWorkspaceMemberRoleParams{WorkspaceID: input.WorkspaceID, UserID: input.ActorUserID, Role: store.WorkspaceRoleModerator}); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	if err := qtx.UpdateWorkspaceMemberRole(ctx, storedb.UpdateWorkspaceMemberRoleParams{WorkspaceID: input.WorkspaceID, UserID: targetID, Role: store.WorkspaceRoleOwner}); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	event, err := insertEvent(ctx, tx, input.WorkspaceID, "", "workspace.ownership_transferred", nil, map[string]string{"workspace_id": input.WorkspaceID, "new_owner_user_id": targetID})
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	workspaceRow, err := qtx.GetWorkspace(ctx, storedb.GetWorkspaceParams{WorkspaceID: input.WorkspaceID, UserID: input.ActorUserID})
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	return storeWorkspaceFromGetWorkspace(workspaceRow), event, tx.Commit()
}

func (s *Store) DeleteWorkspace(ctx context.Context, workspaceID, actorUserID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireWorkspaceOwnerTx(ctx, tx, workspaceID, actorUserID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, workspaceID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

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
	if err := requireNoModerationBlockTx(ctx, tx, ch.WorkspaceID, input.UserID); err != nil {
		return store.Channel{}, store.Event{}, err
	}
	if err := requireChannelAdminTx(ctx, tx, ch.WorkspaceID, input.UserID); err != nil {
		return store.Channel{}, store.Event{}, err
	}
	name := slug(input.Name)
	if name == "" {
		name = ch.Name
	}
	if name != ch.Name && (name == store.GuestChannelName || ch.Name == store.GuestChannelName) {
		return store.Channel{}, store.Event{}, errors.New("guest channel name is reserved")
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
	if err := requireNoModerationBlockTx(ctx, tx, msg.WorkspaceID, input.UserID); err != nil {
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
	affected, err := s.q.WithTx(tx).UpdateMessageBody(ctx, storedb.UpdateMessageBodyParams{Body: body, EditedAt: sqlText(editedAt), ID: msg.ID})
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if affected == 0 {
		return store.Message{}, store.Event{}, errors.New("deleted messages cannot be edited")
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
	if err := requireNoModerationBlockTx(ctx, tx, msg.WorkspaceID, input.UserID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if msg.AuthorID != input.UserID {
		return store.Message{}, store.Event{}, errors.New("only the author can delete a message")
	}
	if msg.DeletedAt != nil {
		return msg, store.Event{}, tx.Commit()
	}
	deletedAt := now()
	affected, err := s.q.WithTx(tx).DeleteMessageBody(ctx, storedb.DeleteMessageBodyParams{DeletedAt: sqlText(deletedAt), ID: msg.ID})
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if affected == 0 {
		msg, err := getMessageTx(ctx, tx, input.MessageID)
		if err != nil {
			return store.Message{}, store.Event{}, err
		}
		return msg, store.Event{}, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_attachments WHERE message_id = ?`, msg.ID); err != nil {
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
