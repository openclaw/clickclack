package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func waitPushLockWaiters(t *testing.T, ctx context.Context, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT a.pid)
			FROM pg_stat_activity a
			WHERE a.wait_event_type = 'Lock' AND a.application_name = current_schema()
		`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("registrations did not reach the lock barrier: wanted %d waiters", want)
}

func TestPushSubscriptionCapSerializesConcurrentRegistrations(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st := newMigratedPostgresTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-concurrent@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for i := range store.MaxPushSubscriptionsPerUser {
		if _, err := st.UpsertPushSubscription(ctx, pushInput(owner.ID, fmt.Sprintf("https://push.example.com/seed/%d", i), "device")); err != nil {
			t.Fatal(err)
		}
	}
	blocker, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx, `
		SELECT id FROM user_push_subscriptions WHERE user_id = $1
		ORDER BY created_at, id LIMIT 1 FOR UPDATE
	`, owner.ID); err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan error, 2)
	for i := range 2 {
		go func() {
			_, err := st.UpsertPushSubscription(ctx, pushInput(owner.ID, fmt.Sprintf("https://push.example.com/new/%d", i), "device"))
			outcomes <- err
		}()
	}
	waitPushLockWaiters(t, ctx, st.db, 2)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-outcomes; err != nil {
			t.Fatal(err)
		}
	}
	devices, err := st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != store.MaxPushSubscriptionsPerUser {
		t.Fatalf("concurrent registrations escaped device cap: got %d, want %d", len(devices), store.MaxPushSubscriptionsPerUser)
	}
}

func TestPushFailureBookkeepingCommitsTogether(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st := newMigratedPostgresTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-atomic@example.com")
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://push.example.com/atomic"
	if _, err := st.UpsertPushSubscription(ctx, pushInput(owner.ID, endpoint, "device")); err != nil {
		t.Fatal(err)
	}
	blocker, err := st.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext(current_schema()), 17)"); err != nil {
		t.Fatal(err)
	}
	defer blocker.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtext(current_schema()), 17)")
	if _, err := st.db.ExecContext(ctx, `
		CREATE FUNCTION hold_push_backoff() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF position('SET next_attempt_at =' in current_query()) > 0 THEN
				PERFORM pg_advisory_xact_lock(hashtext(current_schema()), 17);
			END IF;
			RETURN NULL;
		END;
		$$;
		CREATE TRIGGER hold_push_backoff BEFORE UPDATE ON user_push_subscriptions
		FOR EACH STATEMENT EXECUTE FUNCTION hold_push_backoff()
	`); err != nil {
		t.Fatal(err)
	}
	outcome := make(chan error, 1)
	go func() {
		_, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, endpoint, time.Hour)
		outcome <- err
	}()
	waitPushLockWaiters(t, ctx, st.db, 1)
	devices, err := st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].FailureCount != 0 {
		t.Fatal("failure count became visible before its backoff committed")
	}
	if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_unlock(hashtext(current_schema()), 17)"); err != nil {
		t.Fatal(err)
	}
	if err := <-outcome; err != nil {
		t.Fatal(err)
	}
	devices, err = st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil || len(devices) != 1 || devices[0].FailureCount != 1 {
		t.Fatal("completed failure was not recorded")
	}
}
