package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

const uploadQuotaReservationTTL = 15 * time.Minute

func (s *Store) CreateUpload(ctx context.Context, input store.CreateUploadInput) (store.Upload, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Upload{}, err
	}
	defer tx.Rollback()
	if err := lockUploadQuotaTx(ctx, tx, input.WorkspaceID, input.OwnerID); err != nil {
		return store.Upload{}, err
	}
	if err := canCreateUploadTx(ctx, tx, input.WorkspaceID, input.OwnerID, input.ByteSize); err != nil {
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
	if err := s.q.WithTx(tx).InsertUpload(ctx, storedb.InsertUploadParams{
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
	}); err != nil {
		return store.Upload{}, err
	}
	return upload, tx.Commit()
}

func (s *Store) ReserveUploadQuota(ctx context.Context, workspaceID, userID string, byteSize int64) (store.UploadQuotaReservation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.UploadQuotaReservation{}, err
	}
	defer tx.Rollback()
	if err := lockUploadQuotaTx(ctx, tx, workspaceID, userID); err != nil {
		return store.UploadQuotaReservation{}, err
	}
	qtx := s.q.WithTx(tx)
	reservationNow := time.Now().UTC()
	if err := qtx.DeleteExpiredUploadQuotaReservations(ctx, uploadReservationTime(reservationNow)); err != nil {
		return store.UploadQuotaReservation{}, err
	}
	if byteSize < 0 {
		return store.UploadQuotaReservation{}, errors.New("upload byte size must be non-negative")
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, userID); err != nil {
		return store.UploadQuotaReservation{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, userID); err != nil {
		return store.UploadQuotaReservation{}, err
	}
	quota, err := uploadQuotaTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return store.UploadQuotaReservation{}, err
	}
	if err := quota.CanFit(0); err != nil {
		return store.UploadQuotaReservation{}, err
	}
	reservation := store.UploadQuotaReservation{
		ID:          newID("uqr"),
		WorkspaceID: workspaceID,
		OwnerID:     userID,
		ByteSize:    min(byteSize, quota.RemainingBytes),
		CreatedAt:   uploadReservationTime(reservationNow),
		ExpiresAt:   uploadReservationTime(reservationNow.Add(uploadQuotaReservationTTL)),
	}
	if err := qtx.InsertUploadQuotaReservation(ctx, storedb.InsertUploadQuotaReservationParams{
		ID:          reservation.ID,
		WorkspaceID: reservation.WorkspaceID,
		OwnerID:     reservation.OwnerID,
		ByteSize:    reservation.ByteSize,
		CreatedAt:   reservation.CreatedAt,
		ExpiresAt:   reservation.ExpiresAt,
	}); err != nil {
		return store.UploadQuotaReservation{}, err
	}
	return reservation, tx.Commit()
}

func (s *Store) CreateReservedUpload(ctx context.Context, reservationID string, input store.CreateUploadInput) (store.Upload, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Upload{}, err
	}
	defer tx.Rollback()
	if err := lockUploadQuotaTx(ctx, tx, input.WorkspaceID, input.OwnerID); err != nil {
		return store.Upload{}, err
	}
	qtx := s.q.WithTx(tx)
	if err := qtx.DeleteExpiredUploadQuotaReservations(ctx, uploadReservationTime(time.Now().UTC())); err != nil {
		return store.Upload{}, err
	}
	reservation, err := qtx.GetUploadQuotaReservation(ctx, reservationID)
	if err != nil {
		return store.Upload{}, err
	}
	if reservation.WorkspaceID != input.WorkspaceID || reservation.OwnerID != input.OwnerID {
		return store.Upload{}, errors.New("upload quota reservation does not match upload")
	}
	if err := requireMembershipTx(ctx, tx, input.WorkspaceID, input.OwnerID); err != nil {
		return store.Upload{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, input.WorkspaceID, input.OwnerID); err != nil {
		return store.Upload{}, err
	}
	if input.ByteSize < 0 {
		return store.Upload{}, errors.New("upload byte size must be non-negative")
	}
	if input.ByteSize > reservation.ByteSize {
		return store.Upload{}, store.ErrUploadQuotaExceeded
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
	if err := qtx.InsertUpload(ctx, storedb.InsertUploadParams{
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
	}); err != nil {
		return store.Upload{}, err
	}
	rows, err := qtx.DeleteUploadQuotaReservation(ctx, storedb.DeleteUploadQuotaReservationParams{ID: reservationID, OwnerID: input.OwnerID})
	if err != nil {
		return store.Upload{}, err
	}
	if rows == 0 {
		return store.Upload{}, errors.New("upload quota reservation was not released")
	}
	return upload, tx.Commit()
}

func (s *Store) ReleaseUploadQuotaReservation(ctx context.Context, reservationID, userID string) error {
	_, err := s.q.DeleteUploadQuotaReservation(ctx, storedb.DeleteUploadQuotaReservationParams{ID: reservationID, OwnerID: userID})
	return err
}

func (s *Store) UploadQuota(ctx context.Context, workspaceID, userID string) (store.UploadQuota, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.UploadQuota{}, err
	}
	defer tx.Rollback()
	if err := requireMembershipTx(ctx, tx, workspaceID, userID); err != nil {
		return store.UploadQuota{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, userID); err != nil {
		return store.UploadQuota{}, err
	}
	if err := s.q.WithTx(tx).DeleteExpiredUploadQuotaReservations(ctx, uploadReservationTime(time.Now().UTC())); err != nil {
		return store.UploadQuota{}, err
	}
	return uploadQuotaTx(ctx, tx, workspaceID, userID)
}

func (s *Store) CanCreateUpload(ctx context.Context, workspaceID, userID string, byteSize int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockUploadQuotaTx(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	return canCreateUploadTx(ctx, tx, workspaceID, userID, byteSize)
}

func canCreateUploadTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string, byteSize int64) error {
	if byteSize < 0 {
		return errors.New("upload byte size must be non-negative")
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	quota, err := uploadQuotaTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	return quota.CanFit(byteSize)
}

func lockUploadQuotaTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, "clickclack.upload-quota."+workspaceID, userID)
	return err
}

func uploadQuotaTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) (store.UploadQuota, error) {
	quota := store.UploadQuota{
		MaxBytes: store.UploadQuotaBytesPerUserWorkspace,
		MaxCount: store.UploadQuotaCountPerUserWorkspace,
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM uploads WHERE workspace_id = $1 AND owner_id = $2)
				+ (SELECT COUNT(*) FROM upload_quota_reservations WHERE workspace_id = $3 AND owner_id = $4 AND expires_at > $5),
			(SELECT COALESCE(SUM(byte_size), 0) FROM uploads WHERE workspace_id = $6 AND owner_id = $7)
				+ (SELECT COALESCE(SUM(byte_size), 0) FROM upload_quota_reservations WHERE workspace_id = $8 AND owner_id = $9 AND expires_at > $10)`,
		workspaceID, userID, workspaceID, userID, uploadReservationTime(time.Now().UTC()),
		workspaceID, userID, workspaceID, userID, uploadReservationTime(time.Now().UTC()),
	).Scan(&quota.UsedCount, &quota.UsedBytes); err != nil {
		return store.UploadQuota{}, err
	}
	quota.RemainingCount = max(quota.MaxCount-quota.UsedCount, 0)
	quota.RemainingBytes = max(quota.MaxBytes-quota.UsedBytes, 0)
	return quota, nil
}

func uploadReservationTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
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
	iconVisible, err := workspaceIconUploadVisibleTx(ctx, s.db, upload.WorkspaceID, uploadID)
	if err != nil {
		return store.Upload{}, err
	}
	if iconVisible {
		return upload, nil
	}
	hasLiveAttachments, err := uploadHasLiveAttachmentsTx(ctx, s.db, uploadID)
	if err != nil {
		return store.Upload{}, err
	}
	if !hasLiveAttachments {
		if upload.OwnerID == userID {
			return upload, nil
		}
		return store.Upload{}, errors.New("upload is not visible")
	}
	visible, err := uploadVisibleToUserTx(ctx, s.db, uploadID, userID)
	if err != nil {
		return store.Upload{}, err
	}
	if !visible {
		return store.Upload{}, errors.New("upload is not visible")
	}
	return upload, nil
}

func (s *Store) UploadHasDirectMessageAttachment(ctx context.Context, uploadID string) (bool, error) {
	return s.q.UploadHasDirectMessageAttachment(ctx, uploadID)
}

func (s *Store) UploadHasOtherDirectMessageAttachment(ctx context.Context, uploadID, messageID string) (bool, error) {
	return s.q.UploadHasOtherDirectMessageAttachment(ctx, storedb.UploadHasOtherDirectMessageAttachmentParams{UploadID: uploadID, MessageID: messageID})
}

func (s *Store) AttachUpload(ctx context.Context, input store.AttachUploadInput) (store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return store.Event{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, input.UserID); err != nil {
		return store.Event{}, err
	}
	if msg.AuthorID != input.UserID {
		return store.Event{}, store.ErrMessageNotWritable
	}
	if err := requireNoModerationBlockTx(ctx, tx, msg.WorkspaceID, input.UserID); err != nil {
		return store.Event{}, err
	}
	if msg.DeletedAt != nil {
		return store.Event{}, errors.New("deleted messages cannot have attachments")
	}
	uploadWorkspace, err := qtx.GetUploadWorkspace(ctx, input.UploadID)
	if err != nil {
		return store.Event{}, err
	}
	if uploadWorkspace != msg.WorkspaceID {
		return store.Event{}, errors.New("upload and message workspaces differ")
	}
	uploadRow, err := qtx.GetUpload(ctx, input.UploadID)
	if err != nil {
		return store.Event{}, err
	}
	upload := storeUploadFromGetUpload(uploadRow)
	if upload.OwnerID != input.UserID {
		visible, err := uploadVisibleToUserTx(ctx, tx, input.UploadID, input.UserID)
		if err != nil {
			return store.Event{}, err
		}
		if !visible {
			return store.Event{}, errors.New("upload is not visible")
		}
	}
	rows, err := qtx.AttachUpload(ctx, storedb.AttachUploadParams{
		MessageID: input.MessageID,
		UploadID:  input.UploadID,
		CreatedAt: now(),
	})
	if err != nil {
		return store.Event{}, err
	}
	if rows == 0 {
		return store.Event{}, tx.Commit()
	}
	recipients, err := eventRecipientsForMessageTx(ctx, tx, msg)
	if err != nil {
		return store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, msg.WorkspaceID, msg.ChannelID, "message.updated", msg.ChannelSeq, messagePayload(msg), recipients)
	if err != nil {
		return store.Event{}, err
	}
	return event, tx.Commit()
}

type uploadVisibilityQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func uploadHasLiveAttachmentsTx(ctx context.Context, q uploadVisibilityQueryer, uploadID string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `
		SELECT 1
		FROM message_attachments ma
		JOIN messages m ON m.id = ma.message_id
		WHERE ma.upload_id = $1 AND m.deleted_at IS NULL
		LIMIT 1`, uploadID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func uploadVisibleToUserTx(ctx context.Context, q uploadVisibilityQueryer, uploadID, userID string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `
		SELECT 1
		FROM message_attachments ma
		JOIN messages m ON m.id = ma.message_id
		JOIN workspace_members wm ON wm.workspace_id = m.workspace_id AND wm.user_id = $1
		LEFT JOIN channels c ON c.id = m.channel_id AND c.workspace_id = m.workspace_id
		LEFT JOIN direct_conversation_members dcm ON dcm.conversation_id = m.direct_conversation_id AND dcm.user_id = $2
		WHERE ma.upload_id = $3
		  AND m.deleted_at IS NULL
		  AND (
		    (m.channel_id IS NOT NULL AND (wm.role <> $4 OR c.name = 'guest'))
		    OR (m.direct_conversation_id IS NOT NULL AND wm.role <> $5 AND dcm.user_id IS NOT NULL)
		  )
		LIMIT 1`,
		userID, userID, uploadID, store.WorkspaceRoleGuest, store.WorkspaceRoleGuest,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func workspaceIconUploadVisibleTx(ctx context.Context, q uploadVisibilityQueryer, workspaceID, uploadID string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `
		SELECT 1
		FROM workspaces
		WHERE id = $1 AND icon_url = $2
		LIMIT 1`, workspaceID, "/api/uploads/"+uploadID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) hydrateAttachments(ctx context.Context, messages []store.Message) ([]store.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	ids := make([]string, 0, len(messages))
	indexByID := make(map[string]int, len(messages))
	for i, message := range messages {
		if message.DeletedAt != nil {
			continue
		}
		ids = append(ids, message.ID)
		indexByID[message.ID] = i
	}
	if len(ids) == 0 {
		return messages, nil
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
		JOIN messages m ON m.id = ma.message_id
		WHERE ma.message_id IN (`+placeholders+`) AND m.deleted_at IS NULL
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
