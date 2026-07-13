package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func TestPostgresWriteAuthorizationWaitsForCommittedRevocation(t *testing.T) {
	tests := []struct {
		name  string
		write func(context.Context, *Store, postgresConcurrencyFixture) error
	}{
		{
			name: "channel message",
			write: func(ctx context.Context, st *Store, f postgresConcurrencyFixture) error {
				_, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
					ChannelID: f.channel.ID,
					AuthorID:  f.member.ID,
					Body:      "blocked channel message",
				})
				return err
			},
		},
		{
			name: "direct message",
			write: func(ctx context.Context, st *Store, f postgresConcurrencyFixture) error {
				_, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
					ConversationID: f.direct.ID,
					AuthorID:       f.member.ID,
					Body:           "blocked direct message",
				})
				return err
			},
		},
		{
			name: "upload",
			write: func(ctx context.Context, st *Store, f postgresConcurrencyFixture) error {
				_, err := st.CreateUpload(ctx, store.CreateUploadInput{
					WorkspaceID: f.workspace.ID,
					OwnerID:     f.member.ID,
					Filename:    "blocked.txt",
					ContentType: "text/plain",
					ByteSize:    1,
					StoragePath: "memory://blocked.txt",
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, fixture := newPostgresConcurrencyFixture(t, ctx)
			gate, gatePID := beginPostgresGate(t, ctx, st.db)
			if _, err := gate.ExecContext(ctx,
				`SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`,
				"clickclack.member-write."+fixture.workspace.ID, fixture.member.ID,
			); err != nil {
				t.Fatal(err)
			}

			result := make(chan error, 1)
			go func() {
				result <- tt.write(ctx, st, fixture)
			}()
			waitForPostgresBlockers(t, ctx, st.db, gatePID, 1)

			blockedAt := now()
			if _, err := gate.ExecContext(ctx, `
				INSERT INTO workspace_member_moderation (
					workspace_id, user_id, timeout_until, blocked_at,
					moderation_note, moderation_by, moderation_at
				)
				VALUES ($1, $2, NULL, $3, '', $4, $3)
				ON CONFLICT (workspace_id, user_id) DO UPDATE
				SET blocked_at = excluded.blocked_at,
				    moderation_by = excluded.moderation_by,
				    moderation_at = excluded.moderation_at`,
				fixture.workspace.ID, fixture.member.ID, blockedAt, fixture.owner.ID,
			); err != nil {
				t.Fatal(err)
			}
			if err := gate.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := receivePostgresResult(t, result); !errors.Is(err, store.ErrModerationRestricted) {
				t.Fatalf("write after committed revocation returned %v", err)
			}
		})
	}
}

func TestPostgresModerationWaitsForInFlightWriteAuthorization(t *testing.T) {
	ctx := context.Background()
	st, fixture := newPostgresConcurrencyFixture(t, ctx)
	gate, gatePID := beginPostgresGate(t, ctx, st.db)
	if err := lockMemberWriteAuthorizationTx(ctx, gate, fixture.workspace.ID, fixture.member.ID); err != nil {
		t.Fatal(err)
	}

	blocked := true
	result := make(chan error, 1)
	go func() {
		_, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
			WorkspaceID:  fixture.workspace.ID,
			ActorUserID:  fixture.owner.ID,
			TargetUserID: fixture.member.ID,
			Blocked:      &blocked,
		})
		result <- err
	}()
	waitForPostgresBlockers(t, ctx, st.db, gatePID, 1)
	if err := gate.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := receivePostgresResult(t, result); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresBotRemovalWaitsForInFlightWriteAuthorization(t *testing.T) {
	ctx := context.Background()
	st, fixture := newPostgresConcurrencyFixture(t, ctx)
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: fixture.workspace.ID,
		DisplayName: "Concurrency Bot",
		CreatedBy:   fixture.owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	gate, gatePID := beginPostgresGate(t, ctx, st.db)
	if err := lockMemberWriteAuthorizationTx(ctx, gate, fixture.workspace.ID, bot.ID); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- st.RemoveBotFromWorkspace(ctx, fixture.workspace.ID, bot.ID, fixture.owner.ID)
	}()
	waitForPostgresBlockers(t, ctx, st.db, gatePID, 1)

	if err := requireMembershipTx(ctx, gate, fixture.workspace.ID, bot.ID); err != nil {
		t.Fatalf("bot membership disappeared before writer completed: %v", err)
	}
	if err := gate.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := receivePostgresResult(t, result); err != nil {
		t.Fatal(err)
	}
	if err := st.requireMembership(ctx, fixture.workspace.ID, bot.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("removed bot membership returned %v", err)
	}
}

func TestPostgresGuestClientNonceRecoveryPrecedesQuotaCheck(t *testing.T) {
	tests := []struct {
		name  string
		write func(context.Context, *Store, postgresConcurrencyFixture, store.Channel) (store.Message, string, error)
		retry func(context.Context, *Store, postgresConcurrencyFixture, store.Channel, string) (store.Message, bool, error)
	}{
		{
			name: "channel message",
			write: func(ctx context.Context, st *Store, f postgresConcurrencyFixture, guestChannel store.Channel) (store.Message, string, error) {
				message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
					ChannelID: guestChannel.ID,
					AuthorID:  f.member.ID,
					Body:      "guest nonce retry",
					Nonce:     "guest-message-retry",
				})
				return message, message.ID, err
			},
			retry: func(ctx context.Context, st *Store, f postgresConcurrencyFixture, guestChannel store.Channel, _ string) (store.Message, bool, error) {
				message, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
					ChannelID: guestChannel.ID,
					AuthorID:  f.member.ID,
					Body:      "guest nonce retry",
					Nonce:     "guest-message-retry",
				})
				return message, event.ID == "", err
			},
		},
		{
			name: "thread reply",
			write: func(ctx context.Context, st *Store, f postgresConcurrencyFixture, guestChannel store.Channel) (store.Message, string, error) {
				root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
					ChannelID: guestChannel.ID,
					AuthorID:  f.owner.ID,
					Body:      "guest retry thread",
				})
				if err != nil {
					return store.Message{}, "", err
				}
				reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
					RootMessageID: root.ID,
					AuthorID:      f.member.ID,
					Body:          "guest reply retry",
					Nonce:         "guest-reply-retry",
				})
				return reply, root.ID, err
			},
			retry: func(ctx context.Context, st *Store, f postgresConcurrencyFixture, _ store.Channel, rootID string) (store.Message, bool, error) {
				reply, _, events, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
					RootMessageID: rootID,
					AuthorID:      f.member.ID,
					Body:          "guest reply retry",
					Nonce:         "guest-reply-retry",
				})
				return reply, len(events) == 0, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, fixture, guestChannel := newPostgresGuestConcurrencyFixture(t, ctx)
			created, retryState, err := tt.write(ctx, st, fixture, guestChannel)
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i < store.GuestPostLimit; i++ {
				if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
					ChannelID: guestChannel.ID,
					AuthorID:  fixture.member.ID,
					Body:      fmt.Sprintf("guest quota filler %d", i),
				}); err != nil {
					t.Fatal(err)
				}
			}

			retried, emittedNoEvents, err := tt.retry(ctx, st, fixture, guestChannel, retryState)
			if err != nil {
				t.Fatalf("idempotent retry at quota returned %v", err)
			}
			if retried.ID != created.ID {
				t.Fatalf("idempotent retry returned %q, want %q", retried.ID, created.ID)
			}
			if !emittedNoEvents {
				t.Fatal("idempotent retry emitted duplicate events")
			}
			if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
				ChannelID: guestChannel.ID,
				AuthorID:  fixture.member.ID,
				Body:      "new guest post over quota",
			}); !errors.Is(err, store.ErrPostRateLimited) {
				t.Fatalf("new guest post at quota returned %v", err)
			}
		})
	}
}

func TestPostgresConcurrentGuestClientNonceRecoveryAtQuotaBoundary(t *testing.T) {
	ctx := context.Background()
	st, fixture, guestChannel := newPostgresGuestConcurrencyFixture(t, ctx)
	for i := 0; i < store.GuestPostLimit-1; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: guestChannel.ID,
			AuthorID:  fixture.member.ID,
			Body:      fmt.Sprintf("guest quota seed %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	gate, gatePID := beginPostgresGate(t, ctx, st.db)
	if err := lockGuestPostBudgetTx(ctx, gate, fixture.workspace.ID, fixture.member.ID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		message store.Message
		event   store.Event
		err     error
	}
	results := make(chan result, 2)
	write := func() {
		message, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: guestChannel.ID,
			AuthorID:  fixture.member.ID,
			Body:      "concurrent guest nonce",
			Nonce:     "concurrent-guest-nonce",
		})
		results <- result{message: message, event: event, err: err}
	}
	go write()
	go write()
	waitForPostgresBlockers(t, ctx, st.db, gatePID, 2)
	if err := gate.Rollback(); err != nil {
		t.Fatal(err)
	}

	first := receivePostgresResult(t, results)
	second := receivePostgresResult(t, results)
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent guest retries returned %v and %v", first.err, second.err)
	}
	if first.message.ID == "" || first.message.ID != second.message.ID {
		t.Fatalf("concurrent guest retries returned different messages: %q and %q", first.message.ID, second.message.ID)
	}
	eventCount := 0
	if first.event.ID != "" {
		eventCount++
	}
	if second.event.ID != "" {
		eventCount++
	}
	if eventCount != 1 {
		t.Fatalf("concurrent guest retries emitted %d events, want 1", eventCount)
	}
}

func TestPostgresConcurrentClientNonceRecovery(t *testing.T) {
	tests := []struct {
		name       string
		secondBody string
	}{
		{name: "idempotent", secondBody: "same body"},
		{name: "conflict", secondBody: "different body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, fixture := newPostgresConcurrencyFixture(t, ctx)
			gate, gatePID := beginPostgresGate(t, ctx, st.db)
			if err := lockMessageSequenceTx(ctx, gate, "channel", fixture.channel.ID); err != nil {
				t.Fatal(err)
			}

			type result struct {
				message store.Message
				event   store.Event
				err     error
			}
			results := make(chan result, 2)
			write := func(body string) {
				message, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
					ChannelID: fixture.channel.ID,
					AuthorID:  fixture.member.ID,
					Body:      body,
					Nonce:     "concurrent-nonce",
				})
				results <- result{message: message, event: event, err: err}
			}
			go write("same body")
			go write(tt.secondBody)
			waitForPostgresBlockers(t, ctx, st.db, gatePID, 2)
			if err := gate.Rollback(); err != nil {
				t.Fatal(err)
			}

			first := receivePostgresResult(t, results)
			second := receivePostgresResult(t, results)
			if tt.name == "idempotent" {
				if first.err != nil || second.err != nil {
					t.Fatalf("idempotent writes returned %v and %v", first.err, second.err)
				}
				if first.message.ID == "" || first.message.ID != second.message.ID {
					t.Fatalf("idempotent writes returned different messages: %q and %q", first.message.ID, second.message.ID)
				}
				eventCount := 0
				if first.event.ID != "" {
					eventCount++
				}
				if second.event.ID != "" {
					eventCount++
				}
				if eventCount != 1 {
					t.Fatalf("idempotent writes emitted %d events, want 1", eventCount)
				}
			} else {
				successes := 0
				conflicts := 0
				for _, got := range []result{first, second} {
					switch {
					case got.err == nil:
						successes++
					case errors.Is(got.err, store.ErrClientNonceConflict):
						conflicts++
					default:
						t.Fatalf("unexpected concurrent nonce error: %v", got.err)
					}
				}
				if successes != 1 || conflicts != 1 {
					t.Fatalf("got %d successes and %d conflicts", successes, conflicts)
				}
			}

			var count int
			if err := st.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM messages WHERE author_id = $1 AND client_nonce = $2`,
				fixture.member.ID, "concurrent-nonce",
			).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("persisted %d messages for one client nonce", count)
			}
		})
	}
}

func TestPostgresDeleteAndAttachSerializeOnMessage(t *testing.T) {
	tests := []struct {
		name        string
		attachFirst bool
	}{
		{name: "attach commits before delete", attachFirst: true},
		{name: "delete commits before attach", attachFirst: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, fixture := newPostgresConcurrencyFixture(t, ctx)
			message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
				ChannelID: fixture.channel.ID,
				AuthorID:  fixture.member.ID,
				Body:      "attachment race",
			})
			if err != nil {
				t.Fatal(err)
			}
			upload, err := st.CreateUpload(ctx, store.CreateUploadInput{
				WorkspaceID: fixture.workspace.ID,
				OwnerID:     fixture.member.ID,
				Filename:    "race.txt",
				ContentType: "text/plain",
				ByteSize:    1,
				StoragePath: "memory://race.txt",
			})
			if err != nil {
				t.Fatal(err)
			}

			gate, gatePID := beginPostgresGate(t, ctx, st.db)
			if err := storedb.New(gate).LockMessageForUpdate(ctx, message.ID); err != nil {
				t.Fatal(err)
			}

			attachResult := make(chan struct {
				event store.Event
				err   error
			}, 1)
			deleteResult := make(chan struct {
				event store.Event
				err   error
			}, 1)
			startAttach := func() {
				go func() {
					event, err := st.AttachUpload(ctx, store.AttachUploadInput{
						MessageID: message.ID,
						UploadID:  upload.ID,
						UserID:    fixture.member.ID,
					})
					attachResult <- struct {
						event store.Event
						err   error
					}{event: event, err: err}
				}()
			}
			startDelete := func() {
				go func() {
					_, event, err := st.DeleteMessage(ctx, store.DeleteMessageInput{
						MessageID: message.ID,
						UserID:    fixture.member.ID,
					})
					deleteResult <- struct {
						event store.Event
						err   error
					}{event: event, err: err}
				}()
			}

			if tt.attachFirst {
				startAttach()
			} else {
				startDelete()
			}
			waitForPostgresBlockers(t, ctx, st.db, gatePID, 1)
			if tt.attachFirst {
				startDelete()
			} else {
				startAttach()
			}
			waitForPostgresBlockers(t, ctx, st.db, 0, 2)
			if err := gate.Rollback(); err != nil {
				t.Fatal(err)
			}

			attached := receivePostgresResult(t, attachResult)
			deleted := receivePostgresResult(t, deleteResult)
			if deleted.err != nil || deleted.event.ID == "" {
				t.Fatalf("delete result: event=%#v err=%v", deleted.event, deleted.err)
			}
			if tt.attachFirst {
				if attached.err != nil || attached.event.ID == "" {
					t.Fatalf("attach result: event=%#v err=%v", attached.event, attached.err)
				}
			} else if attached.err == nil {
				t.Fatal("attach succeeded after delete committed")
			}

			var attachmentCount int
			if err := st.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM message_attachments WHERE message_id = $1`,
				message.ID,
			).Scan(&attachmentCount); err != nil {
				t.Fatal(err)
			}
			if attachmentCount != 0 {
				t.Fatalf("deleted message retained %d attachments", attachmentCount)
			}

			events, err := st.ListEventsAfter(ctx, fixture.workspace.ID, fixture.member.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			attachIndex := eventIndex(events, attached.event.ID)
			deleteIndex := eventIndex(events, deleted.event.ID)
			if tt.attachFirst {
				if attachIndex < 0 || deleteIndex < 0 || attachIndex >= deleteIndex {
					t.Fatalf("attach/delete event order was %d/%d", attachIndex, deleteIndex)
				}
			} else if attachIndex >= 0 {
				t.Fatalf("attach emitted event %s after delete", attached.event.ID)
			}
		})
	}
}

type postgresConcurrencyFixture struct {
	owner     store.User
	member    store.User
	workspace store.Workspace
	channel   store.Channel
	direct    store.DirectConversation
}

func newPostgresConcurrencyFixture(t *testing.T, ctx context.Context) (*Store, postgresConcurrencyFixture) {
	t.Helper()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Concurrency Owner",
		Email:       "concurrency-owner-" + suffix + "@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Concurrency Member",
		Email:       "concurrency-member-" + suffix + "@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{
		Name: "Concurrency " + suffix,
		Slug: "concurrency-" + suffix,
	}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Name:        "concurrency",
		Kind:        "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, postgresConcurrencyFixture{
		owner:     owner,
		member:    member,
		workspace: workspace,
		channel:   channel,
		direct:    direct,
	}
}

func newPostgresGuestConcurrencyFixture(t *testing.T, ctx context.Context) (*Store, postgresConcurrencyFixture, store.Channel) {
	t.Helper()
	st, fixture := newPostgresConcurrencyFixture(t, ctx)
	if err := storedb.New(st.db).UpdateWorkspaceMemberRole(ctx, storedb.UpdateWorkspaceMemberRoleParams{
		WorkspaceID: fixture.workspace.ID,
		UserID:      fixture.member.ID,
		Role:        store.WorkspaceRoleGuest,
	}); err != nil {
		t.Fatal(err)
	}
	routeID, err := newRouteID('C')
	if err != nil {
		t.Fatal(err)
	}
	guestChannel := store.Channel{
		ID:          newID("chn"),
		RouteID:     routeID,
		WorkspaceID: fixture.workspace.ID,
		Name:        store.GuestChannelName,
		Kind:        "public",
		CreatedAt:   now(),
	}
	if err := st.q.InsertChannel(ctx, storedb.InsertChannelParams{
		ID:          guestChannel.ID,
		RouteID:     sqlText(guestChannel.RouteID),
		WorkspaceID: guestChannel.WorkspaceID,
		Name:        guestChannel.Name,
		Kind:        guestChannel.Kind,
		CreatedAt:   guestChannel.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	return st, fixture, guestChannel
}

func beginPostgresGate(t *testing.T, ctx context.Context, db *sql.DB) (*sql.Tx, int) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	var pid int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return tx, pid
}

func waitForPostgresBlockers(t *testing.T, ctx context.Context, db *sql.DB, blockerPID, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		var err error
		if blockerPID == 0 {
			err = db.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND cardinality(pg_blocking_pids(pid)) > 0`,
			).Scan(&count)
		} else {
			err = db.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND $1 = ANY(pg_blocking_pids(pid))`,
				blockerPID,
			).Scan(&count)
		}
		if err != nil {
			t.Fatal(err)
		}
		if count >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saw %d blocked transactions, want at least %d", count, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func receivePostgresResult[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Postgres result")
		var zero T
		return zero
	}
}

func eventIndex(events []store.Event, eventID string) int {
	if eventID == "" {
		return -1
	}
	for index, event := range events {
		if event.ID == eventID {
			return index
		}
	}
	return -1
}
