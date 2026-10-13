//go:build clickclack_upgrade_evidence

// This file is copied by scripts/web-push-evidence/upgrade.sh only into trees
// that have push subscriptions: the pull request's previous head and its
// head. The release has none to compile against.
//
// CLICKCLACK_EVIDENCE_MODE=pushseed registers one device of each kind the
// dead-device sweep must judge, through the store API of the tree it runs in,
// for the people already in the database, and then ages the sessions and rows
// the API cannot. CLICKCLACK_EVIDENCE_MODE=pushdigest replays web push
// recipient selection, the way the dispatcher does, for every message and
// writes to CLICKCLACK_EVIDENCE_OUT how many selections include each kind of
// device and a SHA-256 digest of them all. Endpoints, client keys, and session
// tokens never leave the process.
package upgradeevidence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// pushEndpointBase is on a reserved domain: nothing here is ever sent.
const pushEndpointBase = "https://push.upgrade-evidence.invalid/"

// pushKinds are the devices pushseed registers, in order. Each is labeled
// "upgrade evidence <kind>" so the script can find it without its endpoint.
var pushKinds = []string{
	"healthy",
	"revoked",
	"expired",
	"session-deleted",
	"development",
	"failing-recent",
	"failing-old",
}

func init() {
	pushModes["pushseed"] = pushSeed
	pushModes["pushdigest"] = pushDigest
}

func pushSeed(t *testing.T, ctx context.Context, st evidenceStore, _ *sql.DB, dbURL string) {
	writer := openWriter(t, dbURL)
	defer writer.Close()
	people := column(t, writer, `SELECT DISTINCT u.id FROM users u JOIN workspace_members wm ON wm.user_id = u.id WHERE u.kind = 'human' ORDER BY u.id`)
	if len(people) == 0 {
		t.Fatal("the database has no human workspace member")
	}
	current := time.Now().UTC()
	at := func(offset time.Duration) string { return current.Add(offset).Format(time.RFC3339Nano) }
	day := 24 * time.Hour
	for index, kind := range pushKinds {
		userID := people[index%len(people)]
		endpoint := pushEndpointBase + kind
		input := store.PushSubscriptionInput{
			UserID:    userID,
			Endpoint:  endpoint,
			P256dh:    fakePushKey(kind+" p256dh", 65),
			Auth:      fakePushKey(kind+" auth", 16),
			UserAgent: "upgrade evidence " + kind,
		}
		token := ""
		if kind == "development" {
			input.DevelopmentActor = true
		} else {
			session, err := st.CreateSession(ctx, userID)
			if err != nil {
				t.Fatal(err)
			}
			token = session.Token
			input.SessionToken = token
		}
		if _, err := st.UpsertPushSubscription(ctx, input); err != nil {
			t.Fatal(err)
		}
		sessionHash := sha256.Sum256([]byte(token))
		tokenHash := hex.EncodeToString(sessionHash[:])
		switch kind {
		case "healthy":
			if err := st.MarkPushSubscriptionSuccess(ctx, userID, endpoint); err != nil {
				t.Fatal(err)
			}
		case "revoked":
			if err := st.RevokeSession(ctx, token); err != nil {
				t.Fatal(err)
			}
			if count := column(t, writer, rebind(dbURL, `SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND revoked_at IS NOT NULL`), tokenHash); len(count) != 1 || count[0] != "1" {
				t.Fatalf("the revoked session is not revoked: %v", count)
			}
		case "expired":
			execOne(t, writer, dbURL, `UPDATE sessions SET expires_at = ? WHERE token_hash = ?`, at(-time.Hour), tokenHash)
		case "session-deleted":
			execOne(t, writer, dbURL, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
		case "failing-recent":
			// Three refusals, the last asking for a six hour pause, so the
			// device is backing off for the whole run.
			for range 3 {
				if _, err := st.MarkPushSubscriptionFailure(ctx, userID, endpoint, 6*time.Hour); err != nil {
					t.Fatal(err)
				}
			}
		case "failing-old":
			// Refused nine times and quiet for weeks: what the week-of-refusals
			// rule and the retired-key rule would each remove if the row
			// carried what they need.
			for range 9 {
				if _, err := st.MarkPushSubscriptionFailure(ctx, userID, endpoint, 0); err != nil {
					t.Fatal(err)
				}
			}
			execOne(t, writer, dbURL, `UPDATE user_push_subscriptions SET created_at = ?, updated_at = ?, next_attempt_at = ? WHERE endpoint = ?`,
				at(-60*day), at(-40*day), at(-39*day), endpoint)
		}
	}
}

func pushDigest(t *testing.T, ctx context.Context, st evidenceStore, raw *sql.DB, _ string) {
	out := os.Getenv("CLICKCLACK_EVIDENCE_OUT")
	if out == "" {
		t.Fatal("CLICKCLACK_EVIDENCE_OUT is not set")
	}
	kinds := make(map[string]string, len(pushKinds))
	selected := make(map[string]int, len(pushKinds)+1)
	for _, kind := range pushKinds {
		kinds[pushEndpointBase+kind] = kind
		selected[kind] = 0
	}
	messageIDs := column(t, raw, `SELECT id FROM messages ORDER BY id`)
	userIDs := column(t, raw, `SELECT id FROM users ORDER BY id`)
	lines := make([]string, 0, 2*len(messageIDs))
	deliveries, failures := 0, 0
	for _, messageID := range messageIDs {
		for variant, mentioned := range map[string][]string{"plain": nil, "everyone": userIDs} {
			recipients, err := st.ListPushNotificationRecipients(ctx, messageID, mentioned)
			if err != nil {
				lines = append(lines, messageID+"|"+variant+"|error")
				failures++
				continue
			}
			targets := []string{}
			for _, recipient := range recipients {
				if len(recipient.Subscriptions) == 0 {
					continue
				}
				if _, err := st.GetMessage(ctx, messageID, recipient.UserID); err != nil {
					continue
				}
				for _, subscription := range recipient.Subscriptions {
					kind, ok := kinds[subscription.Endpoint]
					if !ok {
						kind = "other"
					}
					selected[kind]++
					targets = append(targets, recipient.UserID+":"+kind)
					deliveries++
				}
			}
			sort.Strings(targets)
			lines = append(lines, messageID+"|"+variant+"|"+strings.Join(targets, ","))
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	result := map[string]any{
		"messages":           len(messageIDs),
		"selections":         len(lines),
		"webpush_deliveries": deliveries,
		"lookup_errors":      failures,
		"selected_by_kind":   selected,
		"digest":             hex.EncodeToString(sum[:]),
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// openWriter opens a handle that may write, for the changes the store API
// has no call for.
func openWriter(t *testing.T, dbURL string) *sql.DB {
	t.Helper()
	driver, source := "pgx", dbURL
	if strings.HasPrefix(dbURL, "sqlite://") {
		driver, source = "sqlite", "file:"+strings.TrimPrefix(dbURL, "sqlite://")
	}
	writer, err := sql.Open(driver, source)
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

// rebind writes ? placeholders as $1, $2, ... for PostgreSQL.
func rebind(dbURL, query string) string {
	if strings.HasPrefix(dbURL, "sqlite://") {
		return query
	}
	var out strings.Builder
	next := 1
	for _, r := range query {
		if r == '?' {
			fmt.Fprintf(&out, "$%d", next)
			next++
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func execOne(t *testing.T, writer *sql.DB, dbURL, query string, args ...any) {
	t.Helper()
	result, err := writer.Exec(rebind(dbURL, query), args...)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("%q changed %d rows, want 1: %v", query, rows, err)
	}
}

// fakePushKey is a base64url string decoding to size bytes, derived from
// label, so every run registers the same keys.
func fakePushKey(label string, size int) string {
	var key []byte
	for block := 0; len(key) < size; block++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s %d", label, block)))
		key = append(key, sum[:]...)
	}
	return base64.RawURLEncoding.EncodeToString(key[:size])
}
