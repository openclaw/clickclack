package fakeco

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestSeedIsIdempotentAndCreatesSyntheticThreads(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), "fakeco.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	first, err := Seed(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Seed(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("seed changed on rerun:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.Version != SeedVersion || first.Workspace.Slug != "fakeco" || len(first.Users) != 3 || len(first.Channels) != 4 || len(first.MessageIDs) != 7 {
		t.Fatalf("unexpected manifest: %#v", first)
	}
	members, err := st.ListWorkspaceMembers(ctx, first.Workspace.ID, first.Users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 synthetic members, got %d", len(members))
	}

	rootID := first.MessageIDs["engineering-rollout"]
	root, replies, state, err := st.GetThread(ctx, rootID, first.Users[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if root.ID != rootID || len(replies) != 1 || replies[0].ID != first.MessageIDs["engineering-rollout-reply"] || state.ReplyCount != 1 {
		t.Fatalf("unexpected seeded thread: root=%#v replies=%#v state=%#v", root, replies, state)
	}
	for _, channel := range first.Channels {
		page, err := st.ListMessages(ctx, channel.ID, first.Users[0].ID, store.MessagePageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) != 1 {
			t.Fatalf("channel %s: expected one deterministic root, got %d", channel.Name, len(page.Messages))
		}
	}
}
