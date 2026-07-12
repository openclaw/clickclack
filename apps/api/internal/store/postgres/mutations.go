package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) UpdateWorkspace(ctx context.Context, input store.UpdateWorkspaceInput) (store.Workspace, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if err := qtx.LockWorkspaceForUpdate(ctx, input.WorkspaceID); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
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
	if err := validateWorkspaceIconURLTx(ctx, tx, input.WorkspaceID, input.ActorUserID, iconURL); err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	if err := qtx.UpdateWorkspace(ctx, storedb.UpdateWorkspaceParams{Name: name, Slug: workspaceSlug, IconUrl: iconURL, ID: input.WorkspaceID}); err != nil {
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
	targetID := strings.TrimSpace(input.NewOwnerUserID)
	if targetID == "" || targetID == input.ActorUserID {
		return store.Workspace{}, store.Event{}, errors.New("new owner must be another workspace member")
	}
	qtx := s.q.WithTx(tx)
	roles, err := qtx.MembershipRolesForUpdate(ctx, storedb.MembershipRolesForUpdateParams{
		WorkspaceID:  input.WorkspaceID,
		ActorUserID:  input.ActorUserID,
		TargetUserID: targetID,
	})
	if err != nil {
		return store.Workspace{}, store.Event{}, err
	}
	actorRole := ""
	targetRole := ""
	for _, role := range roles {
		if role.UserID == input.ActorUserID {
			actorRole = role.Role
		}
		if role.UserID == targetID {
			targetRole = role.Role
		}
	}
	if actorRole != store.WorkspaceRoleOwner {
		return store.Workspace{}, store.Event{}, store.ErrWorkspaceOwnerRequired
	}
	if targetRole == "" {
		return store.Workspace{}, store.Event{}, errors.New("new owner must be a workspace member")
	}
	if targetRole == store.WorkspaceRoleBot || targetRole == store.WorkspaceRoleGuest {
		return store.Workspace{}, store.Event{}, errors.New("new owner must be a human member or moderator")
	}
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

func (s *Store) DeleteWorkspace(ctx context.Context, workspaceID, actorUserID string) ([]store.PendingUploadCleanup, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if err := qtx.LockWorkspaceForUpdate(ctx, workspaceID); err != nil {
		return nil, err
	}
	if err := requireWorkspaceOwnerTx(ctx, tx, workspaceID, actorUserID); err != nil {
		return nil, err
	}
	storagePaths, err := qtx.ListWorkspaceUploadStoragePaths(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	cleanups := make([]store.PendingUploadCleanup, 0, len(storagePaths))
	cleanupTime := now()
	for _, storagePath := range storagePaths {
		cleanup, err := qtx.InsertPendingUploadCleanup(ctx, storedb.InsertPendingUploadCleanupParams{
			ID:          newID("ucl"),
			WorkspaceID: workspaceID,
			StoragePath: storagePath,
			CreatedAt:   cleanupTime,
			UpdatedAt:   cleanupTime,
		})
		if err != nil {
			return nil, err
		}
		cleanups = append(cleanups, pendingUploadCleanupFromRow(cleanup))
	}
	affected, err := qtx.DeleteWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cleanups, nil
}

func (s *Store) ListPendingUploadCleanups(ctx context.Context, limit int) ([]store.PendingUploadCleanup, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.q.ListPendingUploadCleanups(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	cleanups := make([]store.PendingUploadCleanup, 0, len(rows))
	for _, row := range rows {
		cleanups = append(cleanups, pendingUploadCleanupFromRow(row))
	}
	return cleanups, nil
}

func (s *Store) DeletePendingUploadCleanup(ctx context.Context, cleanupID string) error {
	return s.q.DeletePendingUploadCleanup(ctx, cleanupID)
}

func (s *Store) RecordPendingUploadCleanupFailure(ctx context.Context, cleanupID, message string) error {
	return s.q.RecordPendingUploadCleanupFailure(ctx, storedb.RecordPendingUploadCleanupFailureParams{
		ID:        cleanupID,
		LastError: message,
		UpdatedAt: now(),
	})
}

func pendingUploadCleanupFromRow(row storedb.PendingUploadCleanup) store.PendingUploadCleanup {
	return store.PendingUploadCleanup{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		StoragePath: row.StoragePath,
		Attempts:    row.Attempts,
		LastError:   row.LastError,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_attachments WHERE message_id = $1`, msg.ID); err != nil {
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
