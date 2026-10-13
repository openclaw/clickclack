package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// Search rows must be found through message_search_rows: matching the
// unindexed messages_fts.message_id scans the whole index for every deleted
// message, which made deleting a large channel take minutes.
func TestSearchTriggersFindRowsThroughTheRecordedRowid(t *testing.T) {
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "search-rows.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0044_message_search_rows.sql")

	owner, err := st.EnsureBootstrap(ctx, "Search Rows Owner", "search-rows@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	existing, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "rowneedle written before the upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	doomed, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "search-rows-doomed"})
	if err != nil {
		t.Fatal(err)
	}
	removed, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: doomed.ID, AuthorID: owner.ID, Body: "rowneedle written after the upgrade"})
	if err != nil {
		t.Fatal(err)
	}

	// Detach both search rows from their message IDs. Only a rowid lookup can
	// still find them when the messages change.
	for _, id := range []string{existing.ID, removed.ID} {
		result, err := st.db.ExecContext(ctx, `
			UPDATE messages_fts
			SET message_id = 'detached'
			WHERE rowid = (SELECT search_rowid FROM message_search_rows WHERE message_id = ?)`, id)
		if err != nil {
			t.Fatal(err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			t.Fatalf("message %s has no recorded search row", id)
		}
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: existing.ID, UserID: owner.ID, Body: "rowneedle edited"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteChannel(ctx, doomed.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	var detached, searchRows, recordedRows int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages_fts WHERE message_id = 'detached'`).Scan(&detached); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages_fts`).Scan(&searchRows); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_search_rows r JOIN messages_fts f ON f.rowid = r.search_rowid AND f.message_id = r.message_id`).Scan(&recordedRows); err != nil {
		t.Fatal(err)
	}
	if detached != 0 || searchRows != recordedRows {
		t.Fatalf("detached rows = %d, search rows = %d, recorded rows = %d", detached, searchRows, recordedRows)
	}
	page, err := st.SearchMessagePage(ctx, store.SearchPageRequest{WorkspaceID: workspace.ID, UserID: owner.ID, Query: "rowneedle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].ID != existing.ID || page.Results[0].EditedAt == nil {
		t.Fatalf("search after edit and channel deletion = %#v", page.Results)
	}
}
