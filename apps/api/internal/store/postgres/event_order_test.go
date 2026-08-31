package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type eventOrderFixture struct {
	st        *Store
	owner     store.User
	workspace store.Workspace
	channel   store.Channel
}

func newEventOrderFixture(t *testing.T) eventOrderFixture {
	t.Helper()
	ctx := t.Context()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "event-order@example.com")
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
	return eventOrderFixture{st, owner, workspaces[0], channels[0]}
}

type eventOrderResult struct {
	events []store.Event
	err    error
}

func awaitEventOrderResult(t *testing.T, ctx context.Context, result <-chan eventOrderResult) []store.Event {
	t.Helper()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.events
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}

func TestPostgresEventCommitOrder(t *testing.T) {
	for _, scenario := range []string{"commit", "rollback", "private", "reply"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEventOrderFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			root, baseline, err := f.st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.owner.ID, Body: "reply root"})
			if err != nil {
				t.Fatal(err)
			}
			tx := mustBeginPostgresTx(t, ctx, f.st.db)
			var recipients []string
			if scenario == "private" {
				recipients = []string{f.owner.ID}
			}
			first, err := insertEventWithRecipients(ctx, tx, f.workspace.ID, "", "workspace.updated", nil, map[string]string{"workspace_id": f.workspace.ID}, recipients)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan eventOrderResult, 1)
			go func() {
				if scenario == "reply" {
					_, _, events, err := f.st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: f.owner.ID, Body: "two-event reply"})
					done <- eventOrderResult{events, err}
				} else {
					_, event, err := f.st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.owner.ID, Body: "later transaction"})
					done <- eventOrderResult{[]store.Event{event}, err}
				}
			}()
			waitForBlockedPostgresQuery(t, ctx, f.st.db, "LockWorkspaceEventLog")
			if scenario == "rollback" {
				err = tx.Rollback()
			} else {
				err = tx.Commit()
			}
			if err != nil {
				t.Fatal(err)
			}
			later := awaitEventOrderResult(t, ctx, done)
			if len(later) == 0 {
				t.Fatal("missing later events")
			}
			if scenario != "rollback" && later[0].Cursor <= first.Cursor {
				t.Fatalf("commit order inverted: %s then %s", first.Cursor, later[0].Cursor)
			}
			if scenario == "reply" && (len(later) != 2 || later[0].Cursor >= later[1].Cursor) {
				t.Fatalf("reply batch not ordered: %#v", later)
			}
			replay, err := f.st.ListEventsAfter(ctx, f.workspace.ID, f.owner.ID, baseline.Cursor, 20)
			if err != nil {
				t.Fatal(err)
			}
			want := later
			if scenario != "rollback" {
				want = append([]store.Event{first}, later...)
			}
			if len(replay) != len(want) {
				t.Fatalf("replay has %d events, want %d", len(replay), len(want))
			}
			for i := range want {
				if replay[i].ID != want[i].ID {
					t.Fatalf("replay %d=%s want %s", i, replay[i].ID, want[i].ID)
				}
			}
		})
	}
}

func TestPostgresEventLogLeavesOtherWorkspacesIndependent(t *testing.T) {
	f := newEventOrderFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	other, err := f.st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Independent"}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx := mustBeginPostgresTx(t, ctx, f.st.db)
	first, err := insertEvent(ctx, tx, f.workspace.ID, "", "workspace.updated", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The second workspace must commit without releasing the first workspace's fence.
	_, second, err := f.st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: other.ID, UserID: f.owner.ID, Name: "independent"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cursor == first.Cursor {
		t.Fatal("cross-workspace cursor collision")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresEventParentLocksPrecedeLogLock(t *testing.T) {
	for _, parent := range []string{"workspace", "recipient"} {
		t.Run(parent, func(t *testing.T) {
			f := newEventOrderFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			tx := mustBeginPostgresTx(t, ctx, f.st.db)
			query, id, fragment := `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, f.workspace.ID, "LockEventWorkspace"
			if parent == "recipient" {
				query, id, fragment = `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.owner.ID, "LockEventRecipient"
			}
			var locked string
			if err := tx.QueryRowContext(ctx, query, id).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			done := make(chan eventOrderResult, 1)
			go func() {
				secondTx, err := f.st.db.BeginTx(ctx, nil)
				if err != nil {
					done <- eventOrderResult{err: err}
					return
				}
				defer secondTx.Rollback()
				event, err := insertEventWithRecipients(ctx, secondTx, f.workspace.ID, "", "workspace.updated", nil, nil, []string{f.owner.ID})
				if err == nil {
					err = secondTx.Commit()
				}
				done <- eventOrderResult{[]store.Event{event}, err}
			}()
			waitForBlockedPostgresQuery(t, ctx, f.st.db, fragment)
			// If the waiting append owned the log fence, this parent owner would deadlock.
			first, err := insertEventWithRecipients(ctx, tx, f.workspace.ID, "", "workspace.updated", nil, nil, []string{f.owner.ID})
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			second := awaitEventOrderResult(t, ctx, done)
			if second[0].Cursor <= first.Cursor {
				t.Fatalf("parent owner lost event order: %s then %s", first.Cursor, second[0].Cursor)
			}
		})
	}
}

func TestPostgresEventCursorFollowsPrivateFrontier(t *testing.T) {
	f := newEventOrderFixture(t)
	ctx := t.Context()
	tx := mustBeginPostgresTx(t, ctx, f.st.db)
	private, err := insertEventWithRecipients(ctx, tx, f.workspace.ID, "", "workspace.updated", nil, nil, []string{f.owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	const frontier = "cur_7zzzzzzzzz0000000000000000"
	if _, err := f.st.db.ExecContext(ctx, `UPDATE events SET cursor=$1 WHERE id=$2`, frontier, private.ID); err != nil {
		t.Fatal(err)
	}
	_, event, err := f.st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.owner.ID, Body: "after private frontier"})
	if err != nil {
		t.Fatal(err)
	}
	if event.Cursor <= frontier {
		t.Fatalf("cursor %s did not follow private frontier %s", event.Cursor, frontier)
	}
}
