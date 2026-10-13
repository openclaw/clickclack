package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestPushSubscriptionsUpsertAndCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.UpsertPushSubscription(ctx, pushInput(owner.ID, "https://push.example.com/send/1", "iPhone"))
	if err != nil {
		t.Fatal(err)
	}
	if first.UserAgent != "iPhone" || first.FailureCount != 0 || first.LastSuccessAt != nil {
		t.Fatalf("unexpected stored subscription %#v", first)
	}

	again, err := st.UpsertPushSubscription(ctx, pushInput(owner.ID, "https://push.example.com/send/1", "iPhone 17"))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.CreatedAt != first.CreatedAt || again.UserAgent != "iPhone 17" {
		t.Fatalf("re-subscribing must replace in place, got %#v", again)
	}
	subscriptions, err := st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected one device, got %d", len(subscriptions))
	}

	for index := 2; index <= store.MaxPushSubscriptionsPerUser+3; index++ {
		if _, err := st.UpsertPushSubscription(ctx, pushInput(owner.ID, "https://push.example.com/send/"+strconv.Itoa(index), "device")); err != nil {
			t.Fatal(err)
		}
	}
	subscriptions, err = st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != store.MaxPushSubscriptionsPerUser {
		t.Fatalf("expected the cap of %d, got %d", store.MaxPushSubscriptionsPerUser, len(subscriptions))
	}
	if subscriptions[0].ID == first.ID {
		t.Fatal("the oldest device must be evicted first")
	}

	if err := st.DeletePushSubscription(ctx, owner.ID, subscriptions[0].ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil || len(remaining) != store.MaxPushSubscriptionsPerUser {
		t.Fatalf("deleting by id must not remove a row: %d %v", len(remaining), err)
	}
	if err := st.DeletePushSubscription(ctx, owner.ID, "https://push.example.com/send/4"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePushSubscription(ctx, owner.ID, "https://push.example.com/send/4"); err != nil {
		t.Fatalf("deleting twice must be idempotent: %v", err)
	}
	remaining, err = st.ListPushSubscriptions(ctx, owner.ID)
	if err != nil || len(remaining) != store.MaxPushSubscriptionsPerUser-1 {
		t.Fatalf("expected one fewer device: %d %v", len(remaining), err)
	}
}

// A shared device's one subscription moves to whichever account registers it
// last. For the new owner that is a first registration: its created_at is the
// moment it moved, so the log reads "registered" and the ten-device cap does
// not evict it as the owner's oldest device.
func TestPushSubscriptionMovedToAnotherAccountStartsFresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	first, err := st.EnsureBootstrap(ctx, "Owner", "push-move-first@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "push-move-second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://push.example.com/send/shared"
	original, err := st.UpsertPushSubscription(ctx, pushInput(first.ID, endpoint, "laptop"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	registeredFrom := time.Now().UTC()
	moved, err := st.UpsertPushSubscription(ctx, pushInput(second.ID, endpoint, "laptop"))
	if err != nil {
		t.Fatal(err)
	}
	registeredBy := time.Now().UTC()
	createdAt, err := time.Parse(time.RFC3339Nano, moved.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if createdAt.Before(registeredFrom) || createdAt.After(registeredBy) || moved.CreatedAt == original.CreatedAt {
		t.Fatalf("the moved device must carry the second account's registration time, got %q (first registered %q)", moved.CreatedAt, original.CreatedAt)
	}
	if moved.CreatedAt != moved.UpdatedAt {
		t.Fatalf("the move must read as a registration, not a refresh: created %q, updated %q", moved.CreatedAt, moved.UpdatedAt)
	}
	refreshed, err := st.UpsertPushSubscription(ctx, pushInput(second.ID, endpoint, "laptop"))
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.CreatedAt != moved.CreatedAt {
		t.Fatalf("a refresh by the same account keeps created_at: %q, then %q", moved.CreatedAt, refreshed.CreatedAt)
	}
}

func TestPushSubscriptionsRejectBadInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-input@example.com")
	if err != nil {
		t.Fatal(err)
	}
	valid := pushInput(owner.ID, "https://push.example.com/send/1", "device")
	for name, input := range map[string]store.PushSubscriptionInput{
		"no user":     {Endpoint: valid.Endpoint, P256dh: valid.P256dh, Auth: valid.Auth},
		"no endpoint": {UserID: owner.ID, P256dh: valid.P256dh, Auth: valid.Auth},
		"opaque endpoint": {
			UserID: owner.ID, Endpoint: "not a url", P256dh: valid.P256dh, Auth: valid.Auth,
		},
		"no keys": {UserID: owner.ID, Endpoint: valid.Endpoint},
		"bad keys": {
			UserID: owner.ID, Endpoint: valid.Endpoint, P256dh: "not base64!", Auth: valid.Auth,
		},
		"no session": {
			UserID: owner.ID, Endpoint: valid.Endpoint, P256dh: valid.P256dh, Auth: valid.Auth,
		},
	} {
		if _, err := st.UpsertPushSubscription(ctx, input); err == nil {
			t.Fatalf("expected %s to fail", name)
		}
	}
	long := valid
	long.UserAgent = strings.Repeat("l", 400)
	stored, err := st.UpsertPushSubscription(ctx, long)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(stored.UserAgent)) != 200 {
		t.Fatalf("device label must be truncated, got %d runes", len([]rune(stored.UserAgent)))
	}
}

func TestPushSubscriptionsCascadeWithTheUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "push-cascade@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, pushInput(member.ID, "https://push.example.com/send/cascade", "device")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, member.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_push_subscriptions WHERE user_id = ?`, member.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("expected the subscription to be removed with the user, %d left", remaining)
	}
}

func TestPushNotificationRecipientsSeparatePushoverAndWebPush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "recipients-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	pushoverOnly := addPushMember(t, st, workspace.ID, "Pushover", "pushover-only@example.com")
	pushOnly := addPushMember(t, st, workspace.ID, "Push", "push-only@example.com")
	both := addPushMember(t, st, workspace.ID, "Both", "push-both@example.com")

	enablePushover(t, st, pushoverOnly, "p12345678901234567890123456789")
	enablePushover(t, st, both, "b12345678901234567890123456789")
	// The push-only member never saves notification settings, so the settings
	// row does not exist. An inner join here would drop the member entirely.
	if _, err := st.UpsertPushSubscription(ctx, pushInput(pushOnly, "https://push.example.com/send/phone", "phone")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, pushInput(pushOnly, "https://push.example.com/send/laptop", "laptop")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, pushInput(both, "https://push.example.com/send/both", "phone")); err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID,
		AuthorID:  owner.ID,
		Body:      "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	recipients := recipientsByUser(t, st, message.ID, nil)
	if got := recipients[pushoverOnly]; got.PushoverUserKey == "" || len(got.Subscriptions) != 0 {
		t.Fatalf("pushover-only recipient is %#v", got)
	}
	if got := recipients[pushOnly]; got.PushoverUserKey != "" || len(got.Subscriptions) != 2 {
		t.Fatalf("push-only recipient is %#v", got)
	}
	if got := recipients[both]; got.PushoverUserKey == "" || len(got.Subscriptions) != 1 {
		t.Fatalf("combined recipient is %#v", got)
	}
	if got := recipients[owner.ID]; got.UserID != "" {
		t.Fatal("the author must never be a recipient")
	}
	subscription := recipients[both].Subscriptions[0]
	if subscription.Endpoint == "" || subscription.P256dh == "" || subscription.Auth == "" {
		t.Fatalf("delivery target is incomplete: %#v", subscription)
	}

	if err := st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channels[0].ID,
		UserID:     pushOnly,
		Preference: store.ChannelNotifyMuted,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channels[0].ID,
		UserID:     both,
		Preference: store.ChannelNotifyMentions,
	}); err != nil {
		t.Fatal(err)
	}
	recipients = recipientsByUser(t, st, message.ID, nil)
	if _, ok := recipients[pushOnly]; ok {
		t.Fatal("a muted channel must silence web push too")
	}
	if _, ok := recipients[both]; ok {
		t.Fatal("mentions-only must skip an unmentioned member")
	}
	recipients = recipientsByUser(t, st, message.ID, []string{both})
	if got := recipients[both]; len(got.Subscriptions) != 1 {
		t.Fatalf("a mentioned member keeps their devices: %#v", got)
	}
}

func TestPushSubscriptionDeliveryFollowsTheSessionAndBackoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "session-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member := addPushMember(t, st, workspace.ID, "Member", "session-member@example.com")
	session, err := st.CreateSession(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	input := pushInput(member, "https://push.example.com/send/session", "phone")
	input.SessionToken = session.Token
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID,
		AuthorID:  owner.ID,
		Body:      "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := recipientsByUser(t, st, message.ID, nil)[member]; len(got.Subscriptions) != 1 {
		t.Fatalf("a live session must deliver: %#v", got)
	}

	count, err := st.MarkPushSubscriptionFailure(ctx, member, input.Endpoint, 0)
	if err != nil || count != 1 {
		t.Fatalf("failure count is %d: %v", count, err)
	}
	if got := recipientsByUser(t, st, message.ID, nil)[member]; len(got.Subscriptions) != 0 {
		t.Fatal("a backed-off device must be skipped, not deleted")
	}
	subscriptions, err := st.ListPushSubscriptions(ctx, member)
	if err != nil || len(subscriptions) != 1 || subscriptions[0].FailureCount != 1 {
		t.Fatalf("the row must survive a failure: %#v %v", subscriptions, err)
	}
	if err := st.MarkPushSubscriptionSuccess(ctx, member, input.Endpoint); err != nil {
		t.Fatal(err)
	}
	subscriptions, err = st.ListPushSubscriptions(ctx, member)
	if err != nil || subscriptions[0].FailureCount != 0 || subscriptions[0].LastSuccessAt == nil {
		t.Fatalf("success must clear the backoff: %#v %v", subscriptions, err)
	}
	if got := recipientsByUser(t, st, message.ID, nil)[member]; len(got.Subscriptions) != 1 {
		t.Fatal("a recovered device delivers again")
	}

	if err := st.RevokeSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if got := recipientsByUser(t, st, message.ID, nil)[member]; len(got.Subscriptions) != 0 {
		t.Fatal("a signed-out device must stop receiving message text")
	}
}

// TestPushSubscriptionDeliveryIsRereadBeforeSending covers the send-time
// check the delivery worker makes on a push that already sat in its queue.
func TestPushSubscriptionDeliveryIsRereadBeforeSending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "reread-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member := addPushMember(t, st, workspace.ID, "Member", "reread-member@example.com")
	session, err := st.CreateSession(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	input := pushInput(member, "https://push.example.com/send/reread", "phone")
	input.SessionToken = session.Token
	input.DevelopmentActor = false
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}

	target, err := st.GetPushSubscriptionDelivery(ctx, member, input.Endpoint, "")
	if err != nil || target.Endpoint != input.Endpoint || target.P256dh != input.P256dh || target.Auth != input.Auth {
		t.Fatalf("a live device must be deliverable: %#v %v", target, err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, owner.ID, input.Endpoint, ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another user's device must not be found: %v", err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, member, "https://push.example.com/send/unknown", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("an unknown device must not be found: %v", err)
	}

	if _, err := st.MarkPushSubscriptionFailure(ctx, member, input.Endpoint, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, member, input.Endpoint, ""); !errors.Is(err, store.ErrPushSubscriptionBackingOff) {
		t.Fatalf("a backed-off device must wait: %v", err)
	}
	if err := st.MarkPushSubscriptionSuccess(ctx, member, input.Endpoint); err != nil {
		t.Fatal(err)
	}

	if _, err := st.db.ExecContext(ctx, `UPDATE sessions SET expires_at = '2000-01-01T00:00:00Z' WHERE user_id = ?`, member); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, member, input.Endpoint, ""); !errors.Is(err, store.ErrPushSessionEnded) {
		t.Fatalf("an expired session must stop delivery: %v", err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE sessions SET expires_at = '2999-01-01T00:00:00Z' WHERE user_id = ?`, member); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, member, input.Endpoint, ""); !errors.Is(err, store.ErrPushSessionEnded) {
		t.Fatalf("a revoked session must stop delivery: %v", err)
	}

	if err := st.DeletePushSubscription(ctx, member, input.Endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, member, input.Endpoint, ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a deleted device must not be found: %v", err)
	}

	development := pushInput(member, "https://push.example.com/send/development", "laptop")
	if _, err := st.UpsertPushSubscription(ctx, development); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, member, development.Endpoint, ""); err != nil {
		t.Fatalf("a development registration has no session to follow: %v", err)
	}
}

func TestMarkPushSubscriptionFailureIgnoresUnknownRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	count, err := st.MarkPushSubscriptionFailure(ctx, "usr_missing", "https://push.example.com/send/none", 0)
	if err != nil || count != 0 {
		t.Fatalf("unexpected result %d: %v", count, err)
	}
}

func addPushMember(t *testing.T, st *Store, workspaceID, name, email string) string {
	t.Helper()
	user, err := st.CreateUser(context.Background(), store.CreateUserInput{DisplayName: name, Email: email})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(context.Background(), workspaceID, user.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func enablePushover(t *testing.T, st *Store, userID, key string) {
	t.Helper()
	if _, err := st.UpdateCurrentUser(context.Background(), store.UpdateCurrentUserInput{
		UserID: userID,
		NotificationSettings: &store.NotificationSettings{
			PushoverEnabled: true,
			PushoverUserKey: key,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func recipientsByUser(t *testing.T, st *Store, messageID string, mentioned []string) map[string]store.PushNotificationRecipient {
	t.Helper()
	recipients, err := st.ListPushNotificationRecipients(context.Background(), messageID, mentioned)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]store.PushNotificationRecipient, len(recipients))
	for _, recipient := range recipients {
		out[recipient.UserID] = recipient
	}
	return out
}

// pushInput builds a subscription with the RFC 8291 example client key, which
// is a real point on the curve so validation accepts it.
func pushInput(userID, endpoint, label string) store.PushSubscriptionInput {
	return store.PushSubscriptionInput{
		UserID:    userID,
		Endpoint:  endpoint,
		P256dh:    "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:      "BTBZMqHH6r4Tts7J_aSIgg",
		UserAgent: label,
		// Most of these tests are about storage, not authority, so they
		// register the way the local development identity does.
		DevelopmentActor: true,
	}
}

// A device keeps the key it first registered under. Registering the same
// endpoint again, for the same account or another, is the same browser
// subscription, made under the key in force when it first arrived.
func TestPushSubscriptionKeepsTheKeyItRegisteredUnder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	first, err := st.EnsureBootstrap(ctx, "Owner", "push-key-first@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "push-key-second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://push.example.com/send/keyed"
	registered := pushInput(first.ID, endpoint, "phone")
	registered.KeyID = "key-before"
	stored, err := st.UpsertPushSubscription(ctx, registered)
	if err != nil {
		t.Fatal(err)
	}
	if stored.KeyID != "key-before" {
		t.Fatalf("a new device records the key in force: %q", stored.KeyID)
	}
	refreshed := pushInput(first.ID, endpoint, "phone")
	refreshed.KeyID = "key-now"
	if stored, err = st.UpsertPushSubscription(ctx, refreshed); err != nil || stored.KeyID != "key-before" {
		t.Fatalf("a refresh keeps the key the device registered under: %q %v", stored.KeyID, err)
	}
	moved := pushInput(second.ID, endpoint, "phone")
	moved.KeyID = "key-now"
	if stored, err = st.UpsertPushSubscription(ctx, moved); err != nil || stored.KeyID != "key-before" {
		t.Fatalf("a move to another account keeps the key too: %q %v", stored.KeyID, err)
	}
	listed, err := st.ListPushSubscriptions(ctx, second.ID)
	if err != nil || len(listed) != 1 || listed[0].KeyID != "key-before" {
		t.Fatalf("the listed device carries its key: %#v %v", listed, err)
	}

	target, err := st.GetPushSubscriptionDelivery(ctx, second.ID, endpoint, "key-before")
	if err != nil || target.KeyID != "key-before" {
		t.Fatalf("a device under the current key is deliverable: %#v %v", target, err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, second.ID, endpoint, "key-now"); !errors.Is(err, store.ErrPushSubscriptionKeyRetired) {
		t.Fatalf("a device under a retired key must not be sent to: %v", err)
	}

	legacy := pushInput(second.ID, "https://push.example.com/send/unrecorded", "laptop")
	if stored, err = st.UpsertPushSubscription(ctx, legacy); err != nil || stored.KeyID != "" {
		t.Fatalf("a device registered with no key recorded has none: %q %v", stored.KeyID, err)
	}
	if _, err := st.GetPushSubscriptionDelivery(ctx, second.ID, legacy.Endpoint, "key-now"); err != nil {
		t.Fatalf("an unrecorded key is unknown, not retired: %v", err)
	}
}

// failing_since marks when the current run of refusals began and
// last_failure_at when the latest one happened: the first failure stamps both,
// a later one moves only the latest, and a success or registering again ends
// the run.
func TestPushSubscriptionFailingSinceMarksTheStartOfARun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-failing@example.com")
	if err != nil {
		t.Fatal(err)
	}
	input := pushInput(owner.ID, "https://push.example.com/send/failing", "phone")
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}
	run := func() (sql.NullString, sql.NullString) {
		t.Helper()
		var since, last sql.NullString
		if err := st.db.QueryRowContext(ctx, `SELECT failing_since, last_failure_at FROM user_push_subscriptions WHERE endpoint = ?`, input.Endpoint).Scan(&since, &last); err != nil {
			t.Fatal(err)
		}
		return since, last
	}
	if since, last := run(); since.Valid || last.Valid {
		t.Fatal("a new device is not failing")
	}

	before := time.Now().UTC()
	if _, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()
	first, firstLast := run()
	stamped, err := time.Parse(time.RFC3339Nano, first.String)
	if !first.Valid || err != nil || stamped.Before(before) || stamped.After(after) {
		t.Fatalf("the first failure must stamp the run: %#v %v", first, err)
	}
	if firstLast != first {
		t.Fatalf("the first failure is also the latest: %q, latest %q", first.String, firstLast.String)
	}

	time.Sleep(2 * time.Millisecond)
	count, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0)
	if err != nil || count != 2 {
		t.Fatalf("failure count is %d: %v", count, err)
	}
	again, againLast := run()
	if again != first {
		t.Fatalf("a second failure must keep the start of the run: %q, then %q", first.String, again.String)
	}
	latest, err := time.Parse(time.RFC3339Nano, againLast.String)
	if !againLast.Valid || err != nil || !latest.After(stamped) {
		t.Fatalf("a second failure must move the latest refusal: %#v after %q", againLast, first.String)
	}

	// Registering again is the app opening: proof the device is alive.
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}
	if since, last := run(); since.Valid || last.Valid {
		t.Fatalf("registering again must end the run of refusals: %q, latest %q", since.String, last.String)
	}

	if _, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkPushSubscriptionSuccess(ctx, owner.ID, input.Endpoint); err != nil {
		t.Fatal(err)
	}
	if since, last := run(); since.Valid || last.Valid {
		t.Fatalf("a success must end the run: %q, latest %q", since.String, last.String)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0); err != nil {
		t.Fatal(err)
	}
	next, _ := run()
	restarted, err := time.Parse(time.RFC3339Nano, next.String)
	if !next.Valid || err != nil || !restarted.After(latest) {
		t.Fatalf("a failure after a success starts a new run: %#v after %q", next, againLast.String)
	}
}

// Two refusals in one short relay outage and then a quiet week are not a week
// of refusals: retries ride on messages, so nothing was tried in between, and
// the device may be fine. Only a run still refusing at its end is pruned.
func TestPrunePushSubscriptionsNeedsRefusalsThroughTheWeek(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-blip@example.com")
	if err != nil {
		t.Fatal(err)
	}
	input := pushInput(owner.ID, "https://push.example.com/send/blip", "phone")
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}
	stored := func() int {
		t.Helper()
		subscriptions, err := st.ListPushSubscriptions(ctx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(subscriptions)
	}
	for range 2 {
		if _, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	result, err := st.PrunePushSubscriptions(ctx, "", now.Add(8*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Total() != 0 {
		t.Fatalf("two refusals in one blip and a quiet week removed the device: %#v", result)
	}
	if stored() != 1 {
		t.Fatal("the device refused in a blip must be kept")
	}

	// The same run, begun a week ago and refused again now, has refused
	// through the week.
	if _, err := st.db.ExecContext(ctx, `UPDATE user_push_subscriptions SET failing_since = ? WHERE endpoint = ?`, now.Add(-8*24*time.Hour).Format(time.RFC3339Nano), input.Endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0); err != nil {
		t.Fatal(err)
	}
	result, err = st.PrunePushSubscriptions(ctx, "", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result != (store.PushPruneResult{Failing: 1}) {
		t.Fatalf("a device refused through the week must be removed: %#v", result)
	}
	if stored() != 0 {
		t.Fatal("the device refused through the week is still stored")
	}
}

// A device that registers again has been opened: whatever it was refused
// before, a later run is judged from its own start.
func TestPrunePushSubscriptionsJudgesARunFromTheLastRegistration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "push-refresh@example.com")
	if err != nil {
		t.Fatal(err)
	}
	input := pushInput(owner.ID, "https://push.example.com/send/refresh", "phone")
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}
	stored := func() int {
		t.Helper()
		subscriptions, err := st.ListPushSubscriptions(ctx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(subscriptions)
	}
	for range 2 {
		if _, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	// Those two refusals were a week ago.
	if _, err := st.db.ExecContext(ctx, `UPDATE user_push_subscriptions SET failing_since = ? WHERE endpoint = ?`, now.Add(-8*24*time.Hour).Format(time.RFC3339Nano), input.Endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
		t.Fatal(err)
	}
	for want := int64(1); want <= 2; want++ {
		count, err := st.MarkPushSubscriptionFailure(ctx, owner.ID, input.Endpoint, 0)
		if err != nil || count != want {
			t.Fatalf("registering again must restart the count: %d, want %d: %v", count, want, err)
		}
	}

	for _, at := range []time.Time{time.Now().UTC(), now.Add(8 * 24 * time.Hour)} {
		result, err := st.PrunePushSubscriptions(ctx, "", at)
		if err != nil {
			t.Fatal(err)
		}
		if result.Total() != 0 {
			t.Fatalf("a device refused twice since it registered again was removed at %s: %#v", at.Format(time.RFC3339), result)
		}
	}
	if stored() != 1 {
		t.Fatal("the device that registered again must be kept")
	}
}

// TestPrunePushSubscriptionsRemovesOnlyDeadDevices seeds one device of each
// kind a sweep removes beside one of each kind it must keep, including the
// neighbor of every threshold.
func TestPrunePushSubscriptionsRemovesOnlyDeadDevices(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "prune-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "prune-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ago := func(duration time.Duration) string { return now.Add(-duration).Format(time.RFC3339Nano) }
	const day = 24 * time.Hour
	live, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberLive, err := st.CreateSession(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	register := func(userID, name, keyID, sessionToken string) string {
		t.Helper()
		input := pushInput(userID, "https://push.example.com/send/"+name, name)
		input.KeyID = keyID
		input.SessionToken = sessionToken
		input.DevelopmentActor = sessionToken == ""
		if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
			t.Fatal(err)
		}
		return input.Endpoint
	}
	set := func(endpoint, column string, value any) {
		t.Helper()
		if _, err := st.db.ExecContext(ctx, `UPDATE user_push_subscriptions SET `+column+` = ? WHERE endpoint = ?`, value, endpoint); err != nil {
			t.Fatal(err)
		}
	}
	endedSession := func(end func(store.Session)) string {
		t.Helper()
		session, err := st.CreateSession(ctx, member.ID)
		if err != nil {
			t.Fatal(err)
		}
		end(session)
		return session.Token
	}

	healthy := register(owner.ID, "healthy", "key-now", live.Token)
	failingWeek := register(owner.ID, "refused-8-days-through-the-last-hour", "key-now", live.Token)
	set(failingWeek, "failing_since", ago(8*day))
	set(failingWeek, "last_failure_at", ago(time.Hour))
	set(failingWeek, "failure_count", 2)
	// Two refusals in one ten-minute relay blip and then a quiet week:
	// retries ride on messages, so nothing was tried after the blip.
	blip := register(owner.ID, "refused-twice-in-a-blip-8-days-ago", "key-now", live.Token)
	set(blip, "failing_since", ago(8*day))
	set(blip, "last_failure_at", ago(8*day-10*time.Minute))
	set(blip, "failure_count", 2)
	lastRefusedDayAgo := register(owner.ID, "refused-8-days-last-25-hours-ago", "key-now", live.Token)
	set(lastRefusedDayAgo, "failing_since", ago(8*day))
	set(lastRefusedDayAgo, "last_failure_at", ago(25*time.Hour))
	set(lastRefusedDayAgo, "failure_count", 5)
	failingDays := register(owner.ID, "refused-6-days-through-the-last-hour", "key-now", live.Token)
	set(failingDays, "failing_since", ago(6*day))
	set(failingDays, "last_failure_at", ago(time.Hour))
	set(failingDays, "failure_count", 6)
	// One refusal and then a quiet week: no second attempt was ever made,
	// and one refusal never removes a device.
	failedOnce := register(member.ID, "failed-once-8-days-ago", "key-now", memberLive.Token)
	set(failedOnce, "failing_since", ago(8*day))
	set(failedOnce, "last_failure_at", ago(8*day))
	set(failedOnce, "failure_count", 1)
	retiredMonth := register(owner.ID, "retired-31-days", "key-before", live.Token)
	set(retiredMonth, "updated_at", ago(31*day))
	retiredWeeks := register(owner.ID, "retired-29-days", "key-before", live.Token)
	set(retiredWeeks, "updated_at", ago(29*day))
	unrecorded := register(owner.ID, "key-unrecorded", "", live.Token)
	set(unrecorded, "updated_at", ago(400*day))
	development := register(owner.ID, "development", "key-now", "")
	set(development, "updated_at", ago(400*day))
	sessionMissing := register(member.ID, "session-missing", "key-now", endedSession(func(session store.Session) {
		if _, err := st.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, session.ID); err != nil {
			t.Fatal(err)
		}
	}))
	sessionRevoked := register(member.ID, "session-revoked", "key-now", endedSession(func(session store.Session) {
		if err := st.RevokeSession(ctx, session.Token); err != nil {
			t.Fatal(err)
		}
	}))
	sessionExpired := register(member.ID, "session-expired", "key-now", endedSession(func(session store.Session) {
		if _, err := st.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id = ?`, ago(time.Minute), session.ID); err != nil {
			t.Fatal(err)
		}
	}))

	remaining := func() map[string]bool {
		t.Helper()
		out := map[string]bool{}
		for _, userID := range []string{owner.ID, member.ID} {
			rows, err := st.db.QueryContext(ctx, `SELECT endpoint FROM user_push_subscriptions WHERE user_id = ?`, userID)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var endpoint string
				if err := rows.Scan(&endpoint); err != nil {
					t.Fatal(err)
				}
				out[endpoint] = true
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}

	// With no current key known, nothing counts as retired; the other two
	// rules do not need one.
	result, err := st.PrunePushSubscriptions(ctx, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if result != (store.PushPruneResult{Failing: 1, SessionEnded: 3}) || !remaining()[retiredMonth] {
		t.Fatalf("the first pass removes one device refused through a week and three whose session ended, and retires nothing without a current key: %#v", result)
	}

	result, err = st.PrunePushSubscriptions(ctx, "key-now", now)
	if err != nil {
		t.Fatal(err)
	}
	if result != (store.PushPruneResult{RetiredKey: 1}) {
		t.Fatalf("the second pass removes only the retired device the first could not judge: %#v", result)
	}
	kept := remaining()
	for name, endpoint := range map[string]string{
		"healthy": healthy, "failing for 6 days": failingDays, "retired key touched 29 days ago": retiredWeeks,
		"key never recorded": unrecorded, "development row with no session": development,
		"refused once, then a quiet week": failedOnce, "refused twice in a blip, then a quiet week": blip,
		"refused for 8 days, last 25 hours ago": lastRefusedDayAgo,
	} {
		if !kept[endpoint] {
			t.Fatalf("the sweep removed the %s device", name)
		}
	}
	for name, endpoint := range map[string]string{
		"refused for 8 days through the last hour": failingWeek, "retired key untouched for 31 days": retiredMonth,
		"session missing": sessionMissing, "session revoked": sessionRevoked, "session expired": sessionExpired,
	} {
		if kept[endpoint] {
			t.Fatalf("the sweep kept the %s device", name)
		}
	}
	if len(kept) != 8 {
		t.Fatalf("expected eight devices left, got %v", kept)
	}

	if result, err = st.PrunePushSubscriptions(ctx, "key-now", now); err != nil || result.Total() != 0 {
		t.Fatalf("a second sweep finds nothing: %#v %v", result, err)
	}
}
