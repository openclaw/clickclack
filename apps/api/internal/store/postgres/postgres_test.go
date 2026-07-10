package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/requestmeta"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestWorkspaceMemberPageMigrationUpgradesExistingData(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	applyPostgresMigrationsBefore(t, ctx, st, "0013_workspace_member_page_indexes.sql")

	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO users (id, display_name, handle, created_at)
		VALUES
			('usr_owner', 'Owner', 'owner', $1),
			('usr_unicode', 'ÉLODIE', 'ÉLODIE-HANDLE', $2)`,
		now(), now(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, name, slug, created_at)
		VALUES ('wsp_upgrade', 'Upgrade', 'upgrade', $1)`,
		now(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES
			('wsp_upgrade', 'usr_owner', 'owner', $1),
			('wsp_upgrade', 'usr_unicode', 'member', $2)`,
		now(), now(),
	); err != nil {
		t.Fatal(err)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var roleSort int
	var sortName, sortHandle string
	if err := st.db.QueryRowContext(ctx, `
		SELECT role_sort, sort_name, sort_handle
		FROM workspace_members
		WHERE workspace_id = 'wsp_upgrade' AND user_id = 'usr_unicode'`,
	).Scan(&roleSort, &sortName, &sortHandle); err != nil {
		t.Fatal(err)
	}
	if roleSort != 2 || sortName != "élodie" || sortHandle != "élodie-handle" {
		t.Fatalf("unexpected migrated sort keys: role=%d name=%q handle=%q", roleSort, sortName, sortHandle)
	}
	var indexCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname = 'idx_workspace_members_page'`,
	).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("expected workspace member page index, got %d", indexCount)
	}

	page, err := st.ListWorkspaceMemberPage(ctx, "wsp_upgrade", "usr_owner", store.WorkspaceMemberPageRequest{
		Query: "ÉLO",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount == nil || *page.TotalCount != 1 || len(page.Members) != 1 || page.Members[0].User.ID != "usr_unicode" {
		t.Fatalf("unexpected migrated member page: %#v", page)
	}
}

func TestPostgresStoreSmoke(t *testing.T) {
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	ctx := context.Background()
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Postgres Owner", Email: "pg-owner-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Postgres Smoke " + suffix}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "pg-smoke", Kind: "public"})
	if err != nil {
		t.Fatal(err)
	}
	messageCtx := requestmeta.WithCorrelationID(ctx, "corr-postgres-message")
	created, event, err := st.CreateMessage(messageCtx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "hello postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID == "" || event.Seq == nil || *event.Seq != 1 {
		t.Fatalf("unexpected event: %#v", event)
	}
	assertPostgresEventPayloadValue(t, event, "correlation_id", "corr-postgres-message")
	assertPostgresEventPayloadMissing(t, event, "body")
	page, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != created.ID {
		t.Fatalf("unexpected messages: %#v", page.Messages)
	}
	replyCtx := requestmeta.WithCorrelationID(ctx, "corr-postgres-reply")
	_, state, replyEvents, err := st.CreateThreadReply(replyCtx, store.CreateThreadReplyInput{RootMessageID: created.ID, AuthorID: owner.ID, Body: "postgres thread reply"})
	if err != nil || state.ReplyCount != 1 {
		t.Fatalf("unexpected thread reply result: %#v err=%v", state, err)
	}
	if len(replyEvents) != 2 || replyEvents[0].Type != "thread.reply_created" {
		t.Fatalf("unexpected thread reply events: %#v", replyEvents)
	}
	assertPostgresEventPayloadValue(t, replyEvents[0], "correlation_id", "corr-postgres-reply")
	assertPostgresEventPayloadMissing(t, replyEvents[1], "correlation_id")
	persisted, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	persistedByID := make(map[string]store.Event, len(persisted))
	for _, persistedEvent := range persisted {
		persistedByID[persistedEvent.ID] = persistedEvent
	}
	assertPostgresEventPayloadValue(t, persistedByID[event.ID], "correlation_id", "corr-postgres-message")
	assertPostgresEventPayloadValue(t, persistedByID[replyEvents[0].ID], "correlation_id", "corr-postgres-reply")
	threadPage, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(threadPage.Messages) != 1 || threadPage.Messages[0].ThreadState == nil || threadPage.Messages[0].ThreadState.ReplyCount != 1 {
		t.Fatalf("expected hydrated thread state in postgres message page, got %#v", threadPage.Messages)
	}
	results, err := st.SearchMessages(ctx, workspace.ID, channel.ID, owner.ID, "postgres", 10)
	if err != nil {
		t.Fatal(err)
	}
	foundCreated := false
	for _, result := range results {
		if result.Message.ID == created.ID {
			foundCreated = true
			break
		}
	}
	if !foundCreated {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func newIsolatedPostgresTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Ping(); err != nil {
		_ = adminDB.Close()
		t.Fatal(err)
	}
	schema := fmt.Sprintf("member_upgrade_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec(`CREATE SCHEMA ` + schema); err != nil {
		_ = adminDB.Close()
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		_, _ = adminDB.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = adminDB.Close()
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	st, err := Open(parsed.String())
	if err != nil {
		_, _ = adminDB.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = adminDB.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		_, _ = adminDB.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = adminDB.Close()
	})
	return st
}

func applyPostgresMigrationsBefore(t *testing.T, ctx context.Context, st *Store, cutoff string) {
	t.Helper()
	if _, err := st.db.ExecContext(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		if name >= cutoff {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := st.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES ($1, $2)`, name, now()); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s migration record: %v", name, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func assertPostgresEventPayloadValue(t *testing.T, event store.Event, key, want string) {
	t.Helper()
	got, ok := postgresEventPayloadValue(event, key)
	if !ok || got != want {
		t.Fatalf("event %s payload[%q] = %q, %v; want %q", event.ID, key, got, ok, want)
	}
}

func assertPostgresEventPayloadMissing(t *testing.T, event store.Event, key string) {
	t.Helper()
	if got, ok := postgresEventPayloadValue(event, key); ok {
		t.Fatalf("event %s unexpectedly has payload[%q] = %q", event.ID, key, got)
	}
}

func postgresEventPayloadValue(event store.Event, key string) (string, bool) {
	switch payload := event.Payload.(type) {
	case map[string]string:
		value, ok := payload[key]
		return value, ok
	case map[string]any:
		value, ok := payload[key].(string)
		return value, ok
	default:
		return "", false
	}
}

func TestPostgresConcurrentChannelMessages(t *testing.T) {
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	ctx := context.Background()
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Postgres Owner", Email: "pg-concurrent-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Postgres Concurrent " + suffix}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "pg-concurrent", Kind: "public"})
	if err != nil {
		t.Fatal(err)
	}

	const count = 24
	start := make(chan struct{})
	errs := make(chan error, count)
	seqs := make(chan int64, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			msg, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
				ChannelID: channel.ID,
				AuthorID:  owner.ID,
				Body:      "concurrent postgres message " + time.Now().UTC().Format(time.RFC3339Nano),
			})
			if err != nil {
				errs <- err
				return
			}
			if msg.ChannelSeq == nil {
				t.Errorf("message %d has nil channel seq", i)
				return
			}
			seqs <- *msg.ChannelSeq
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(seqs)
	for err := range errs {
		t.Fatal(err)
	}
	got := make([]int64, 0, count)
	for seq := range seqs {
		got = append(got, seq)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != count {
		t.Fatalf("got %d messages, want %d: %v", len(got), count, got)
	}
	for i, seq := range got {
		want := int64(i + 1)
		if seq != want {
			t.Fatalf("seq[%d] = %d, want %d; all seqs: %v", i, seq, want, got)
		}
	}
}
