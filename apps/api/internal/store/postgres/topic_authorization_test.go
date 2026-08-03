package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestTopicAuthorizationFollowsGuestChannelVisibility(t *testing.T) {
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
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Topic Moderator", Email: "topic-mod-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Topic Guest", Email: "topic-guest-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var guestChannelID, generalChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "guest":
			guestChannelID = channel.ID
		case "general":
			generalChannelID = channel.ID
		}
	}
	if guestChannelID == "" || generalChannelID == "" {
		t.Fatalf("expected guest and general channels, got %#v", channels)
	}

	global, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, Name: "global-" + suffix, CreatedBy: moderator.ID})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: guestChannelID, Name: "visible-" + suffix, CreatedBy: moderator.ID})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: generalChannelID, Name: "hidden-" + suffix, CreatedBy: moderator.ID})
	if err != nil {
		t.Fatal(err)
	}

	topics, err := st.ListTopics(ctx, workspace.ID, guest.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, topic := range topics {
		got[topic.ID] = true
	}
	if !got[global.ID] || !got[visible.ID] {
		t.Fatalf("guest lost visible topics: %#v", topics)
	}
	if got[hidden.ID] {
		t.Fatalf("guest enumerated hidden channel topic: %#v", hidden)
	}

	if _, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: generalChannelID, Name: "intrusion-" + suffix, CreatedBy: guest.ID}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("guest created hidden channel topic, got %v", err)
	}
	if _, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, Name: "global-intrusion-" + suffix, CreatedBy: guest.ID}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("guest created a workspace-global topic, got %v", err)
	}
	if _, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: guestChannelID, Name: "guest-visible-" + suffix, CreatedBy: guest.ID}); err != nil {
		t.Fatalf("guest should retain topic creation in the visible guest channel: %v", err)
	}

	timeoutUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: guest.ID,
		TimeoutUntil: &timeoutUntil,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: guestChannelID, Name: "timed-out-" + suffix, CreatedBy: guest.ID}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("timed-out guest created a topic, got %v", err)
	}

	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: guest.ID,
		ClearTimeout: true,
		Blocked:      &blocked,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: guestChannelID, Name: "blocked-" + suffix, CreatedBy: guest.ID}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("blocked guest created a topic, got %v", err)
	}

	blocked = false
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: guest.ID,
		Blocked:      &blocked,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < store.GuestPostLimit; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: guest.ID, Body: "budget"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: workspace.ID, ChannelID: guestChannelID, Name: "rate-limited-" + suffix, CreatedBy: guest.ID}); !errors.Is(err, store.ErrPostRateLimited) {
		t.Fatalf("rate-limited guest created a topic, got %v", err)
	}
}
