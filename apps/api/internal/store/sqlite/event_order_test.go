package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestEventCursorFollowsPrivateFrontierAfterReopen(t *testing.T) {
	ctx := context.Background()
	dsn := "sqlite://" + filepath.Join(t.TempDir(), "events.db")
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "frontier@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "before reopen"})
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := st.MarkChannelRead(ctx, channels[0].ID, owner.ID, *message.ChannelSeq)
	if err != nil {
		t.Fatal(err)
	}
	// A previous process can leave a cursor ahead of this process's clock/entropy.
	future := "cur_" + strings.ToLower(ulid.MustNew(ulid.Timestamp(time.Now().Add(time.Hour)), nil).String())
	result, err := st.db.ExecContext(ctx, `UPDATE events SET cursor = ? WHERE id = ?`, future, private.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("seeded frontier rows=%d, error=%v", rows, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	member, err := reopened.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "frontier-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.AddWorkspaceMember(ctx, workspaces[0].ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	_, event, err := reopened.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: member.ID, Body: "after reopen"})
	if err != nil {
		t.Fatal(err)
	}
	if event.Cursor <= future {
		t.Fatalf("new cursor %s did not follow private persisted frontier %s", event.Cursor, future)
	}
	events, err := reopened.ListEventsAfter(ctx, workspaces[0].ID, owner.ID, future, 10)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("replay lost post-reopen event: %#v, %v", events, err)
	}
}
