package postgres

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

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
	created, event, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "hello postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID == "" || event.Seq == nil || *event.Seq != 1 {
		t.Fatalf("unexpected event: %#v", event)
	}
	page, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != created.ID {
		t.Fatalf("unexpected messages: %#v", page.Messages)
	}
	if _, state, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: created.ID, AuthorID: owner.ID, Body: "postgres thread reply"}); err != nil || state.ReplyCount != 1 {
		t.Fatalf("unexpected thread reply result: %#v err=%v", state, err)
	}
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
	if len(results) != 1 || results[0].Message.ID != created.ID {
		t.Fatalf("unexpected search results: %#v", results)
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
