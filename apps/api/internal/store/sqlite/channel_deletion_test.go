package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store/channeldeletiontest"
)

func TestDeleteChannelRemovesOwnedContent(t *testing.T) {
	t.Parallel()
	st := newTestStore(t)
	channeldeletiontest.DeleteChannelRemovesOwnedContent(t, st, sqliteChannelDeletionHooks(st))
}

func TestDeleteChannelEnforcesGuardRails(t *testing.T) {
	t.Parallel()
	channeldeletiontest.DeleteChannelEnforcesGuardRails(t, newTestStore(t))
}

func sqliteChannelDeletionHooks(st *Store) channeldeletiontest.Hooks {
	return channeldeletiontest.Hooks{
		OwnedRows: func(t *testing.T, channelID string, messageIDs []string) map[string]int64 {
			t.Helper()
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(messageIDs)), ",")
			ids := make([]any, 0, len(messageIDs))
			for _, id := range messageIDs {
				ids = append(ids, id)
			}
			counts := map[string]int64{}
			for label, query := range map[string]string{
				"messages":              `SELECT COUNT(*) FROM messages WHERE id IN (` + placeholders + `)`,
				"search rows":           `SELECT COUNT(*) FROM messages_fts WHERE message_id IN (` + placeholders + `)`,
				"search row map":        `SELECT COUNT(*) FROM message_search_rows WHERE message_id IN (` + placeholders + `)`,
				"reactions":             `SELECT COUNT(*) FROM reactions WHERE message_id IN (` + placeholders + `)`,
				"attachments":           `SELECT COUNT(*) FROM message_attachments WHERE message_id IN (` + placeholders + `)`,
				"thread state":          `SELECT COUNT(*) FROM thread_state WHERE root_message_id IN (` + placeholders + `)`,
				"pins":                  `SELECT COUNT(*) FROM pinned_messages WHERE message_id IN (` + placeholders + `)`,
				"channel rows":          `SELECT COUNT(*) FROM channels WHERE id = ?`,
				"channel topics":        `SELECT COUNT(*) FROM topics WHERE channel_id = ?`,
				"read pointers":         `SELECT COUNT(*) FROM channel_reads WHERE channel_id = ?`,
				"notification settings": `SELECT COUNT(*) FROM channel_notification_settings WHERE channel_id = ?`,
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

func TestCascadeChildKeysUseIndexes(t *testing.T) {
	t.Parallel()
	st := newTestStore(t)
	// Each deleted parent row looks up its children by these keys.
	for query, index := range map[string]string{
		`SELECT 1 FROM messages WHERE parent_message_id = ?`: "idx_messages_parent_message",
		`SELECT 1 FROM messages WHERE quoted_message_id = ?`: "idx_messages_quoted_message",
	} {
		rows, err := st.db.QueryContext(context.Background(), `EXPLAIN QUERY PLAN `+query, "child_key")
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(plan, "\n"), index) {
			t.Fatalf("%s does not use %s: %v", query, index, plan)
		}
	}
}
