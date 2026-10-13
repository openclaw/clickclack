//go:build clickclack_upgrade_evidence

// Package upgradeevidence is copied by scripts/web-push-evidence/upgrade.sh
// into apps/api/internal/upgradeevidence of two exported trees, the release
// tag and the branch head, so the same code asks both versions the same
// question about one database. It is never built by the normal test run.
//
// CLICKCLACK_EVIDENCE_MODE=seed writes a small populated workspace through the
// store API. CLICKCLACK_EVIDENCE_MODE=pushover turns Pushover on, with made-up
// keys, for every user of an existing database and varies channel preferences,
// so a real database whose users never set Pushover up still exercises
// recipient selection. CLICKCLACK_EVIDENCE_MODE=digest replays Pushover recipient
// selection, the way the server's dispatcher does with web push off, for every
// message in the database and writes counts and a SHA-256 digest to
// CLICKCLACK_EVIDENCE_OUT. Message text, emails, and Pushover keys never leave
// the process: keys are hashed, and only counts and digests are written. The
// modes about registered devices live in push_test.go, which is copied only
// into trees that have push subscriptions.
package upgradeevidence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	postgresstore "github.com/openclaw/clickclack/apps/api/internal/store/postgres"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

type evidenceStore interface {
	store.Store
	Close() error
}

// pushModes holds the modes push_test.go registers, when it is present.
var pushModes = map[string]func(t *testing.T, ctx context.Context, st evidenceStore, raw *sql.DB, dbURL string){}

func TestUpgradeEvidence(t *testing.T) {
	dbURL := os.Getenv("CLICKCLACK_EVIDENCE_DB")
	if dbURL == "" {
		t.Skip("CLICKCLACK_EVIDENCE_DB is not set")
	}
	ctx := context.Background()
	st, raw := open(t, dbURL)
	defer st.Close()
	defer raw.Close()
	switch mode := os.Getenv("CLICKCLACK_EVIDENCE_MODE"); mode {
	case "seed":
		seed(t, ctx, st)
	case "pushover":
		enablePushover(t, ctx, st, raw)
	case "digest":
		digest(t, ctx, st, raw, os.Getenv("CLICKCLACK_EVIDENCE_OUT"))
	default:
		run, ok := pushModes[mode]
		if !ok {
			t.Fatalf("unknown CLICKCLACK_EVIDENCE_MODE %q", mode)
		}
		run(t, ctx, st, raw, dbURL)
	}
}

func open(t *testing.T, dbURL string) (evidenceStore, *sql.DB) {
	t.Helper()
	if strings.HasPrefix(dbURL, "sqlite://") {
		st, err := sqlitestore.Open(dbURL)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := sql.Open("sqlite", "file:"+strings.TrimPrefix(dbURL, "sqlite://")+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		return st, raw
	}
	st, err := postgresstore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatal(err)
	}
	return st, raw
}

func seed(t *testing.T, ctx context.Context, st evidenceStore) {
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@upgrade.test")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("the bootstrap workspace has no channel: %v", err)
	}
	general := channels[0]
	second, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "launches", Kind: "public", UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	type member struct {
		name     string
		pushover string
		enabled  bool
	}
	members := []member{
		{"Ada", "a12345678901234567890123456789", true},
		{"Bea", "b12345678901234567890123456789", true},
		{"Cy", "c12345678901234567890123456789", false},
		{"Di", "", false},
		{"Ed", "e12345678901234567890123456789", true},
		{"Flo", "f12345678901234567890123456789", true},
	}
	ids := map[string]string{}
	for _, entry := range members {
		user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: entry.name, Email: strings.ToLower(entry.name) + "@upgrade.test"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspace.ID, user.ID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
		ids[entry.name] = user.ID
		if entry.pushover != "" {
			if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
				UserID:               user.ID,
				NotificationSettings: &store.NotificationSettings{PushoverEnabled: entry.enabled, PushoverUserKey: entry.pushover},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, preference := range []struct {
		channel, user, value string
	}{
		{general.ID, ids["Bea"], store.ChannelNotifyMuted},
		{general.ID, ids["Ed"], store.ChannelNotifyMentions},
		{second.ID, ids["Ada"], store.ChannelNotifyMentions},
		{second.ID, ids["Flo"], store.ChannelNotifyAll},
	} {
		if err := st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{ChannelID: preference.channel, UserID: preference.user, Preference: preference.value}); err != nil {
			t.Fatal(err)
		}
	}
	authors := []string{owner.ID, ids["Ada"], ids["Cy"], ids["Flo"]}
	for index := range 24 {
		channel := general.ID
		if index%3 == 0 {
			channel = second.ID
		}
		body := fmt.Sprintf("seeded message %d", index)
		if index%5 == 0 {
			body += " @ed"
		}
		root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel, AuthorID: authors[index%len(authors)], Body: body})
		if err != nil {
			t.Fatal(err)
		}
		if index%4 == 0 {
			if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: ids["Bea"], Body: "seeded reply"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	direct, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: ids["Ada"], MemberIDs: []string{ids["Ed"]}})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 4 {
		author := ids["Ada"]
		if index%2 == 1 {
			author = ids["Ed"]
		}
		if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: direct.ID, AuthorID: author, Body: fmt.Sprintf("seeded direct %d", index)}); err != nil {
			t.Fatal(err)
		}
	}
}

func enablePushover(t *testing.T, ctx context.Context, st evidenceStore, raw *sql.DB) {
	userIDs := column(t, raw, `SELECT id FROM users ORDER BY id`)
	channelIDs := column(t, raw, `SELECT id FROM channels ORDER BY id`)
	for index, userID := range userIDs {
		if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
			UserID:               userID,
			NotificationSettings: &store.NotificationSettings{PushoverEnabled: true, PushoverUserKey: fmt.Sprintf("u%029d", index)},
		}); err != nil {
			t.Fatal(err)
		}
		for channelIndex, channelID := range channelIDs {
			preference := []string{store.ChannelNotifyMuted, store.ChannelNotifyMentions, ""}[(index+channelIndex)%3]
			if preference == "" {
				continue
			}
			// A user who cannot see the channel is refused; that is expected.
			_ = st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{ChannelID: channelID, UserID: userID, Preference: preference})
		}
	}
}

func digest(t *testing.T, ctx context.Context, st evidenceStore, raw *sql.DB, out string) {
	if out == "" {
		t.Fatal("CLICKCLACK_EVIDENCE_OUT is not set")
	}
	messageIDs := column(t, raw, `SELECT id FROM messages ORDER BY id`)
	userIDs := column(t, raw, `SELECT id FROM users ORDER BY id`)
	lines := make([]string, 0, 2*len(messageIDs))
	deliveries, failures, withKey := 0, 0, map[string]struct{}{}
	for _, messageID := range messageIDs {
		// Once with no mentions, once with everyone mentioned, so both the
		// all and the mentions-only preference paths are exercised.
		for variant, mentioned := range map[string][]string{"plain": nil, "everyone": userIDs} {
			recipients, err := st.ListPushNotificationRecipients(ctx, messageID, mentioned)
			if err != nil {
				lines = append(lines, messageID+"|"+variant+"|error")
				failures++
				continue
			}
			targets := make([]string, 0, len(recipients))
			for _, recipient := range recipients {
				if recipient.PushoverUserKey == "" {
					continue
				}
				// The dispatcher also requires message access before it notifies.
				if _, err := st.GetMessage(ctx, messageID, recipient.UserID); err != nil {
					continue
				}
				sum := sha256.Sum256([]byte(recipient.PushoverUserKey))
				targets = append(targets, recipient.UserID+":"+hex.EncodeToString(sum[:6]))
				withKey[recipient.UserID] = struct{}{}
				deliveries++
			}
			sort.Strings(targets)
			lines = append(lines, messageID+"|"+variant+"|"+strings.Join(targets, ","))
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	result := map[string]any{
		"messages":            len(messageIDs),
		"selections":          len(lines),
		"pushover_deliveries": deliveries,
		"pushover_recipients": len(withKey),
		"lookup_errors":       failures,
		"digest":              hex.EncodeToString(sum[:]),
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if detail := os.Getenv("CLICKCLACK_EVIDENCE_DETAIL"); detail != "" {
		if err := os.WriteFile(detail, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func column(t *testing.T, raw *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := raw.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
