package postgres

import (
	"context"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) CreateUpload(ctx context.Context, input store.CreateUploadInput) (store.Upload, error) {
	if err := s.requireMembership(ctx, input.WorkspaceID, input.OwnerID); err != nil {
		return store.Upload{}, err
	}
	upload := store.Upload{
		ID:          newID("upl"),
		WorkspaceID: input.WorkspaceID,
		OwnerID:     input.OwnerID,
		Filename:    input.Filename,
		ContentType: input.ContentType,
		ByteSize:    input.ByteSize,
		Width:       input.Width,
		Height:      input.Height,
		DurationMS:  input.DurationMS,
		StoragePath: input.StoragePath,
		CreatedAt:   now(),
	}
	return upload, s.q.InsertUpload(ctx, storedb.InsertUploadParams{
		ID:          upload.ID,
		WorkspaceID: upload.WorkspaceID,
		OwnerID:     upload.OwnerID,
		Filename:    upload.Filename,
		ContentType: upload.ContentType,
		ByteSize:    upload.ByteSize,
		Width:       int64(upload.Width),
		Height:      int64(upload.Height),
		DurationMs:  int64(upload.DurationMS),
		StoragePath: upload.StoragePath,
		CreatedAt:   upload.CreatedAt,
	})
}

func (s *Store) GetUpload(ctx context.Context, uploadID, userID string) (store.Upload, error) {
	row, err := s.q.GetUpload(ctx, uploadID)
	if err != nil {
		return store.Upload{}, err
	}
	upload := storeUploadFromGetUpload(row)
	if err := s.requireMembership(ctx, upload.WorkspaceID, userID); err != nil {
		return store.Upload{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT message_id FROM message_attachments WHERE upload_id = $1`, uploadID)
	if err != nil {
		return store.Upload{}, err
	}
	defer rows.Close()
	messageIDs := []string{}
	for rows.Next() {
		var messageID string
		if err := rows.Scan(&messageID); err != nil {
			return store.Upload{}, err
		}
		messageIDs = append(messageIDs, messageID)
	}
	if err := rows.Err(); err != nil {
		return store.Upload{}, err
	}
	if err := rows.Close(); err != nil {
		return store.Upload{}, err
	}
	for _, messageID := range messageIDs {
		if _, err := s.GetMessage(ctx, messageID, userID); err == nil {
			return upload, nil
		}
	}
	if len(messageIDs) > 0 {
		return store.Upload{}, errors.New("upload is not visible")
	}
	if upload.OwnerID != userID {
		return store.Upload{}, errors.New("upload is not visible")
	}
	return upload, nil
}

func (s *Store) AttachUpload(ctx context.Context, input store.AttachUploadInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, input.UserID); err != nil {
		return err
	}
	uploadWorkspace, err := qtx.GetUploadWorkspace(ctx, input.UploadID)
	if err != nil {
		return err
	}
	if uploadWorkspace != msg.WorkspaceID {
		return errors.New("upload and message workspaces differ")
	}
	uploadRow, err := qtx.GetUpload(ctx, input.UploadID)
	if err != nil {
		return err
	}
	upload := storeUploadFromGetUpload(uploadRow)
	if upload.OwnerID != input.UserID {
		rows, err := tx.QueryContext(ctx, `SELECT message_id FROM message_attachments WHERE upload_id = $1`, input.UploadID)
		if err != nil {
			return err
		}
		messageIDs := []string{}
		for rows.Next() {
			var messageID string
			if err := rows.Scan(&messageID); err != nil {
				rows.Close()
				return err
			}
			messageIDs = append(messageIDs, messageID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		visible := false
		for _, messageID := range messageIDs {
			attachedMessage, err := getMessageTx(ctx, tx, messageID)
			if err == nil && requireMessageAccessTx(ctx, tx, attachedMessage, input.UserID) == nil {
				visible = true
				break
			}
		}
		if !visible {
			return errors.New("upload is not visible")
		}
	}
	if err := qtx.AttachUpload(ctx, storedb.AttachUploadParams{
		MessageID: input.MessageID,
		UploadID:  input.UploadID,
		CreatedAt: now(),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) hydrateAttachments(ctx context.Context, messages []store.Message) ([]store.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	ids := make([]string, 0, len(messages))
	indexByID := make(map[string]int, len(messages))
	for i, message := range messages {
		ids = append(ids, message.ID)
		indexByID[message.ID] = i
	}
	placeholders := pgPlaceholders(len(ids), 1)
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ma.message_id, u.id, u.workspace_id, u.owner_id, u.filename, u.content_type, u.byte_size, u.width, u.height, u.duration_ms, u.storage_path, u.created_at
		FROM message_attachments ma
		JOIN uploads u ON u.id = ma.upload_id
		WHERE ma.message_id IN (`+placeholders+`)
		ORDER BY ma.message_id, ma.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var upload store.Upload
		if err := rows.Scan(&messageID, &upload.ID, &upload.WorkspaceID, &upload.OwnerID, &upload.Filename, &upload.ContentType, &upload.ByteSize, &upload.Width, &upload.Height, &upload.DurationMS, &upload.StoragePath, &upload.CreatedAt); err != nil {
			return nil, err
		}
		if index, ok := indexByID[messageID]; ok {
			messages[index].Attachments = append(messages[index].Attachments, upload)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}
