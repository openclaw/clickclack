package postgres

import (
	"context"
	"database/sql"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

type channelDeletionPlan struct {
	channel store.Channel
	counts  store.ChannelDeletionCounts
	uploads []storedb.ListChannelExclusiveUploadsRow
	blocker error
}

func (s *Store) PreviewChannelDeletion(ctx context.Context, channelID, actorUserID string) (store.ChannelDeletionPreview, error) {
	// Not read-only: the owner check takes a key-share lock on the membership row.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return store.ChannelDeletionPreview{}, err
	}
	defer tx.Rollback()
	plan, err := loadChannelDeletionPlanTx(ctx, tx, channelID, actorUserID)
	if err != nil {
		return store.ChannelDeletionPreview{}, err
	}
	return store.ChannelDeletionPreview{
		Channel: plan.channel,
		Counts:  plan.counts,
		Blocker: store.ChannelDeletionBlockerCode(plan.blocker),
	}, nil
}

func (s *Store) DeleteChannel(ctx context.Context, channelID, actorUserID string) (store.ChannelDeletion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ChannelDeletion{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	workspaceID, err := qtx.GetChannelWorkspace(ctx, channelID)
	if err != nil {
		return store.ChannelDeletion{}, err
	}
	// Workspace first, as DeleteWorkspace and UpdateWorkspace do. Competing
	// channel deletions then cannot both pass the last-channel check, and the
	// icon cannot move onto an upload this deletion removes. NO KEY UPDATE
	// leaves event appends and child inserts, which take KEY SHARE, unblocked.
	if _, err := qtx.LockWorkspaceForChannelDeletion(ctx, workspaceID); err != nil {
		return store.ChannelDeletion{}, err
	}
	// Take the root-message sequence lock before the row lock, in the same order
	// as CreateMessage, so a concurrent send finishes first or fails its foreign
	// key check after this deletion commits.
	if err := lockMessageSequenceTx(ctx, tx, "channel", channelID); err != nil {
		return store.ChannelDeletion{}, err
	}
	if _, err := qtx.LockChannelForDeletion(ctx, channelID); err != nil {
		return store.ChannelDeletion{}, err
	}
	plan, err := loadChannelDeletionPlanTx(ctx, tx, channelID, actorUserID)
	if err != nil {
		return store.ChannelDeletion{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, plan.channel.WorkspaceID, actorUserID); err != nil {
		return store.ChannelDeletion{}, err
	}
	if plan.blocker != nil {
		return store.ChannelDeletion{}, plan.blocker
	}
	if err := lockExclusiveUploadsTx(ctx, tx, &plan); err != nil {
		return store.ChannelDeletion{}, err
	}
	cleanups := make([]store.PendingUploadCleanup, 0, len(plan.uploads))
	cleanupTime := now()
	for _, upload := range plan.uploads {
		if upload.StoragePath != "" {
			cleanup, err := qtx.InsertPendingUploadCleanup(ctx, storedb.InsertPendingUploadCleanupParams{
				ID:          newID("ucl"),
				WorkspaceID: plan.channel.WorkspaceID,
				StoragePath: upload.StoragePath,
				CreatedAt:   cleanupTime,
				UpdatedAt:   cleanupTime,
			})
			if err != nil {
				return store.ChannelDeletion{}, err
			}
			cleanups = append(cleanups, pendingUploadCleanupFromRow(cleanup))
		}
		if err := qtx.DeleteUpload(ctx, upload.ID); err != nil {
			return store.ChannelDeletion{}, err
		}
	}
	// Messages, replies, reactions, pins, attachments, channel topics, read
	// pointers, and notification settings cascade from the channel row.
	affected, err := qtx.DeleteChannel(ctx, plan.channel.ID)
	if err != nil {
		return store.ChannelDeletion{}, err
	}
	if affected == 0 {
		return store.ChannelDeletion{}, sql.ErrNoRows
	}
	// Durable events keep only identifiers and stay in the log so every client
	// cursor remains valid. Live delivery already skips events whose channel is
	// gone, so the deletion itself is workspace-scoped.
	event, err := insertEvent(ctx, tx, plan.channel.WorkspaceID, "", "channel.deleted", nil, map[string]string{
		"channel_id": plan.channel.ID,
		"deleted_by": actorUserID,
	})
	if err != nil {
		return store.ChannelDeletion{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.ChannelDeletion{}, err
	}
	return store.ChannelDeletion{Channel: plan.channel, Counts: plan.counts, Event: event, Cleanups: cleanups}, nil
}

// lockExclusiveUploadsTx locks the planned uploads and keeps only those still
// used by this channel alone. An attachment elsewhere that committed before the
// lock is visible to the second read; one that has not committed waits for this
// deletion and then fails its foreign key check.
func lockExclusiveUploadsTx(ctx context.Context, tx *sql.Tx, plan *channelDeletionPlan) error {
	if len(plan.uploads) == 0 {
		return nil
	}
	qtx := storedb.New(tx)
	ids := make([]string, 0, len(plan.uploads))
	for _, upload := range plan.uploads {
		ids = append(ids, upload.ID)
	}
	if _, err := qtx.LockUploadsForDeletion(ctx, ids); err != nil {
		return err
	}
	uploads, err := qtx.ListChannelExclusiveUploads(ctx, storedb.ListChannelExclusiveUploadsParams{
		WorkspaceID: plan.channel.WorkspaceID,
		ChannelID:   sqlText(plan.channel.ID),
	})
	if err != nil {
		return err
	}
	locked := make(map[string]bool, len(ids))
	for _, id := range ids {
		locked[id] = true
	}
	plan.uploads = plan.uploads[:0]
	plan.counts.Files = 0
	plan.counts.FileBytes = 0
	for _, upload := range uploads {
		if !locked[upload.ID] {
			continue
		}
		plan.uploads = append(plan.uploads, upload)
		plan.counts.Files++
		plan.counts.FileBytes += upload.ByteSize
	}
	return nil
}

func loadChannelDeletionPlanTx(ctx context.Context, tx *sql.Tx, channelID, actorUserID string) (channelDeletionPlan, error) {
	qtx := storedb.New(tx)
	row, err := qtx.GetChannel(ctx, channelID)
	if err != nil {
		return channelDeletionPlan{}, err
	}
	channel := storeChannelFromGetChannel(row)
	if err := requireWorkspaceOwnerTx(ctx, tx, channel.WorkspaceID, actorUserID); err != nil {
		return channelDeletionPlan{}, err
	}
	channelCount, err := qtx.CountWorkspaceChannels(ctx, channel.WorkspaceID)
	if err != nil {
		return channelDeletionPlan{}, err
	}
	workspaceSlug, err := qtx.GetWorkspaceSlug(ctx, channel.WorkspaceID)
	if err != nil {
		return channelDeletionPlan{}, err
	}
	content, err := qtx.CountChannelDeletionContent(ctx, sqlText(channel.ID))
	if err != nil {
		return channelDeletionPlan{}, err
	}
	uploads, err := qtx.ListChannelExclusiveUploads(ctx, storedb.ListChannelExclusiveUploadsParams{
		WorkspaceID: channel.WorkspaceID,
		ChannelID:   sqlText(channel.ID),
	})
	if err != nil {
		return channelDeletionPlan{}, err
	}
	counts := store.ChannelDeletionCounts{
		Messages:      content.Messages,
		ThreadReplies: content.ThreadReplies,
		Pins:          content.Pins,
		Topics:        content.Topics,
		Files:         int64(len(uploads)),
	}
	for _, upload := range uploads {
		counts.FileBytes += upload.ByteSize
	}
	return channelDeletionPlan{
		channel: channel,
		counts:  counts,
		uploads: uploads,
		blocker: store.ChannelDeletionBlocker(workspaceSlug, channel.Name, channelCount),
	}, nil
}
