package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/channeldeletiontest"
	"github.com/openclaw/clickclack/apps/api/internal/store/storetest"
)

func TestDeleteChannelRemovesOwnedContent(t *testing.T) {
	st := newMigratedPostgresChannelDeletionStore(t)
	channeldeletiontest.DeleteChannelRemovesOwnedContent(t, st, postgresChannelDeletionHooks(st))
}

func TestDeleteChannelEnforcesGuardRails(t *testing.T) {
	channeldeletiontest.DeleteChannelEnforcesGuardRails(t, newMigratedPostgresChannelDeletionStore(t))
}

func newMigratedPostgresChannelDeletionStore(t *testing.T) *Store {
	t.Helper()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func postgresChannelDeletionHooks(st *Store) channeldeletiontest.Hooks {
	return channeldeletiontest.Hooks{
		OwnedRows: func(t *testing.T, channelID string, messageIDs []string) map[string]int64 {
			t.Helper()
			placeholders := make([]string, 0, len(messageIDs))
			ids := make([]any, 0, len(messageIDs))
			for index, id := range messageIDs {
				placeholders = append(placeholders, fmt.Sprintf("$%d", index+1))
				ids = append(ids, id)
			}
			in := strings.Join(placeholders, ",")
			counts := map[string]int64{}
			for label, query := range map[string]string{
				"messages":              `SELECT COUNT(*) FROM messages WHERE id IN (` + in + `)`,
				"reactions":             `SELECT COUNT(*) FROM reactions WHERE message_id IN (` + in + `)`,
				"attachments":           `SELECT COUNT(*) FROM message_attachments WHERE message_id IN (` + in + `)`,
				"thread state":          `SELECT COUNT(*) FROM thread_state WHERE root_message_id IN (` + in + `)`,
				"pins":                  `SELECT COUNT(*) FROM pinned_messages WHERE message_id IN (` + in + `)`,
				"channel rows":          `SELECT COUNT(*) FROM channels WHERE id = $1`,
				"channel topics":        `SELECT COUNT(*) FROM topics WHERE channel_id = $1`,
				"read pointers":         `SELECT COUNT(*) FROM channel_reads WHERE channel_id = $1`,
				"notification settings": `SELECT COUNT(*) FROM channel_notification_settings WHERE channel_id = $1`,
			} {
				args := ids
				if !strings.Contains(query, "IN (") {
					args = []any{channelID}
				}
				var count int64
				if err := st.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
					t.Fatalf("%s: %v", label, err)
				}
				counts[label] = count
			}
			return counts
		},
	}
}

func TestCascadeChildKeyIndexesExist(t *testing.T) {
	st := newMigratedPostgresChannelDeletionStore(t)
	for _, index := range []string{"idx_messages_parent_message", "idx_messages_quoted_message"} {
		var exists bool
		if err := st.db.QueryRowContext(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("missing cascade index %s", index)
		}
	}
}

// Two deletions of a workspace's last two channels must not both pass the
// last-channel check. The held workspace lock lines both requests up at their
// first shared lock, then lets them race.
func TestDeleteChannelSerializesLastChannelDecisions(t *testing.T) {
	ctx := context.Background()
	st := newMigratedPostgresChannelDeletionStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "channel-race-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Race", Slug: "channel-race"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var channelIDs []string
	for _, name := range []string{"alpha", "beta"} {
		channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		channelIDs = append(channelIDs, channel.ID)
	}

	holder := beginLockHolder(t, st, `SELECT id FROM workspaces WHERE id = $1 FOR UPDATE`, workspace.ID)
	results := make(chan error, len(channelIDs))
	for _, channelID := range channelIDs {
		go func() {
			_, err := st.DeleteChannel(ctx, channelID, owner.ID)
			results <- err
		}()
	}
	waitForWaitingBackends(t, st, len(channelIDs))
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}

	var deleted, blocked int
	for range channelIDs {
		switch err := <-results; {
		case err == nil:
			deleted++
		case errors.Is(err, store.ErrLastChannel):
			blocked++
		default:
			t.Fatalf("unexpected deletion error: %v", err)
		}
	}
	remaining, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || blocked != 1 || len(remaining) != 1 {
		t.Fatalf("deleted=%d blocked=%d remaining=%d, want one deletion, one last-channel refusal, one channel", deleted, blocked, len(remaining))
	}
}

// An upload attached elsewhere while a deletion is in progress must survive, or
// the attachment must fail; it must never succeed and then lose its file.
func TestDeleteChannelKeepsUploadsAttachedDuringDeletion(t *testing.T) {
	ctx := context.Background()
	st := newMigratedPostgresChannelDeletionStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "upload-race-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Upload race", Slug: "upload-race"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	doomed, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	kept, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "kept"})
	if err != nil {
		t.Fatal(err)
	}
	doomedMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: doomed.ID, AuthorID: owner.ID, Body: "only here for now"})
	if err != nil {
		t.Fatal(err)
	}
	keptMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: kept.ID, AuthorID: owner.ID, Body: "attach it here too"})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := storetest.CreateUpload(ctx, st, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "shared-later.txt",
		ContentType: "text/plain",
		ByteSize:    7,
		StoragePath: "upload-race/shared-later.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: doomedMessage.ID, UploadID: upload.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}

	// Pause the deletion after it chose the upload and before it removes it.
	holder := beginLockHolder(t, st, `LOCK TABLE pending_upload_cleanups IN SHARE MODE`)
	deletion := make(chan error, 1)
	go func() {
		_, err := st.DeleteChannel(ctx, doomed.ID, owner.ID)
		deletion <- err
	}()
	waitForWaitingBackends(t, st, 1)
	attach := make(chan error, 1)
	go func() {
		_, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: keptMessage.ID, UploadID: upload.ID, UserID: owner.ID})
		attach <- err
	}()
	// The attachment either commits now or waits behind the deletion's upload lock.
	var attachErr error
	attachDone := false
	deadline := time.Now().Add(10 * time.Second)
	for !attachDone && waitingBackends(t, st) < 2 {
		select {
		case attachErr = <-attach:
			attachDone = true
		case <-time.After(20 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("attachment neither finished nor waited")
			}
		}
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-deletion; err != nil {
		t.Fatalf("deletion error: %v", err)
	}
	if !attachDone {
		attachErr = <-attach
	}

	var attachments int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_attachments WHERE message_id = $1 AND upload_id = $2`, keptMessage.ID, upload.ID).Scan(&attachments); err != nil {
		t.Fatal(err)
	}
	_, uploadErr := st.GetUpload(ctx, upload.ID, owner.ID)
	if attachErr == nil && (attachments != 1 || uploadErr != nil) {
		t.Fatalf("attachment succeeded but kept %d rows and upload lookup error %v", attachments, uploadErr)
	}
	if attachErr != nil && uploadErr == nil {
		t.Fatalf("attachment failed (%v) but the exclusive upload was not removed", attachErr)
	}
}

func beginLockHolder(t *testing.T, st *Store, query string, args ...any) *sql.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
	return tx
}

// waitForWaitingBackends waits until this test's connections have want lock
// waits, whichever lock holder they queue behind.
func waitForWaitingBackends(t *testing.T, st *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for waiting := waitingBackends(t, st); waiting < want; waiting = waitingBackends(t, st) {
		if time.Now().After(deadline) {
			t.Fatalf("%d backends waiting on locks, want %d", waiting, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitingBackends(t *testing.T, st *Store) int {
	t.Helper()
	var waiting int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pg_stat_activity WHERE cardinality(pg_blocking_pids(pid)) > 0 AND application_name = current_setting('application_name')`).Scan(&waiting); err != nil {
		t.Fatal(err)
	}
	return waiting
}
