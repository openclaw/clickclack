package sqlite

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestListEventDeliveryAttemptsPaginatesWithStableTiebreak(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "delivery-pagination@example.com")
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
	subscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID: workspace.ID,
		EventTypes:  []string{"message.created"},
		CallbackURL: "https://example.com/events",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, 4)
	eventIDs := make(map[string]string, 4)
	for i := range 4 {
		_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channels[0].ID,
			AuthorID:  owner.ID,
			Body:      "delivery",
		})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := st.CreateEventDeliveryAttempt(ctx, store.CreateEventDeliveryAttemptInput{
			SubscriptionID: subscription.ID,
			EventID:        event.ID,
			WorkspaceID:    workspace.ID,
			EventType:      event.Type,
			ResponseStatus: 200 + i,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, attempt.ID)
		eventIDs[attempt.ID] = event.ID
	}
	if _, err := st.db.ExecContext(ctx, `
		UPDATE event_delivery_attempts
		SET created_at = '2026-07-14T12:00:00Z'
		WHERE subscription_id = ?`, subscription.ID); err != nil {
		t.Fatal(err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	first, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, owner.ID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != ids[0] || first[1].ID != ids[1] {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, owner.ID, 2, first[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].ID != ids[2] || second[1].ID != ids[3] {
		t.Fatalf("unexpected second page: %#v", second)
	}
	last, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, owner.ID, 2, second[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 0 {
		t.Fatalf("expected final page to be empty, got %#v", last)
	}

	staleCursor := first[1].ID
	if _, err := st.db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, eventIDs[staleCursor]); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, owner.ID, 2, staleCursor); !errors.Is(err, store.ErrInvalidEventDeliveryCursor) {
		t.Fatalf("expected deleted delivery cursor to be rejected, got %v", err)
	}
	remaining, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, owner.ID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 3 {
		t.Fatalf("expected older delivery attempts to remain after cursor pruning, got %#v", remaining)
	}
}
