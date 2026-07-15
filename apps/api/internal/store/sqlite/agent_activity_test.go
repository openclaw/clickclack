package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// TestAgentActivityMessageKindRoundTrip verifies that the kind + turn_id columns
// added by migration 0017 survive an insert/read cycle, default to 'message',
// and reject unknown kinds.
func TestAgentActivityMessageKindRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
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
	channel := channels[0]

	// Default kind is 'message' with no turn id.
	plain, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Kind != store.MessageKindMessage {
		t.Fatalf("expected default kind %q, got %q", store.MessageKindMessage, plain.Kind)
	}
	if plain.TurnID != "" {
		t.Fatalf("expected empty turn id for ordinary message, got %q", plain.TurnID)
	}

	// Activity kinds round-trip with their turn id.
	for _, kind := range []string{store.MessageKindAgentCommentary, store.MessageKindAgentTool} {
		created, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "activity " + kind, Kind: kind, TurnID: "turn-1"})
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		if created.Kind != kind {
			t.Fatalf("expected kind %q, got %q", kind, created.Kind)
		}
		if created.TurnID != "turn-1" {
			t.Fatalf("expected turn id to round-trip, got %q", created.TurnID)
		}
		fetched, err := st.GetMessage(ctx, created.ID, owner.ID)
		if err != nil {
			t.Fatalf("get %s: %v", kind, err)
		}
		if fetched.Kind != kind || fetched.TurnID != "turn-1" {
			t.Fatalf("re-read lost kind/turn: %#v", fetched)
		}
	}

	// Unknown kinds are rejected.
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "bad", Kind: "bogus"}); err != store.ErrInvalidMessageKind {
		t.Fatalf("expected ErrInvalidMessageKind for unknown kind, got %v", err)
	}
}

func TestAgentActivityNonceReplayIncludesKindAndTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
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

	channelInput := store.CreateMessageInput{
		ChannelID: channels[0].ID,
		AuthorID:  owner.ID,
		Body:      "same body",
		Nonce:     "channel-activity-nonce",
		Kind:      store.MessageKindAgentCommentary,
		TurnID:    "turn-1",
	}
	created, _, err := st.CreateMessage(ctx, channelInput)
	if err != nil {
		t.Fatal(err)
	}
	replayed, event, err := st.CreateMessage(ctx, channelInput)
	if err != nil || replayed.ID != created.ID || event.ID != "" {
		t.Fatalf("expected exact channel replay, got message=%#v event=%#v err=%v", replayed, event, err)
	}
	changedKind := channelInput
	changedKind.Kind = store.MessageKindAgentTool
	if _, _, err := st.CreateMessage(ctx, changedKind); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected channel kind mismatch conflict, got %v", err)
	}
	changedTurn := channelInput
	changedTurn.TurnID = "turn-2"
	if _, _, err := st.CreateMessage(ctx, changedTurn); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected channel turn mismatch conflict, got %v", err)
	}

	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{other.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dmInput := store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "same dm body",
		Nonce:          "dm-activity-nonce",
		Kind:           store.MessageKindAgentTool,
		TurnID:         "turn-dm-1",
	}
	dmCreated, _, err := st.CreateDirectMessage(ctx, dmInput)
	if err != nil {
		t.Fatal(err)
	}
	dmReplayed, dmEvent, err := st.CreateDirectMessage(ctx, dmInput)
	if err != nil || dmReplayed.ID != dmCreated.ID || dmEvent.ID != "" {
		t.Fatalf("expected exact DM replay, got message=%#v event=%#v err=%v", dmReplayed, dmEvent, err)
	}
	dmChangedKind := dmInput
	dmChangedKind.Kind = store.MessageKindAgentCommentary
	if _, _, err := st.CreateDirectMessage(ctx, dmChangedKind); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected DM kind mismatch conflict, got %v", err)
	}
	dmChangedTurn := dmInput
	dmChangedTurn.TurnID = "turn-dm-2"
	if _, _, err := st.CreateDirectMessage(ctx, dmChangedTurn); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected DM turn mismatch conflict, got %v", err)
	}
}

// TestAgentActivityMessagesExemptFromUnread verifies that durable agent activity
// rows authored by another member do not bump the reader's unread count, while
// an ordinary message from the same author does.
func TestAgentActivityMessagesExemptFromUnread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	unreadFor := func(userID string) int64 {
		t.Helper()
		list, err := st.ListChannels(ctx, workspace.ID, userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range list {
			if c.ID == channel.ID {
				return c.UnreadCount
			}
		}
		t.Fatalf("channel %s not found for user %s", channel.ID, userID)
		return 0
	}

	base := unreadFor(owner.ID)

	// An activity message from another member must NOT bump the owner's unread.
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: other.ID, Body: "commentary line", Kind: store.MessageKindAgentCommentary, TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: other.ID, Body: "tool line", Kind: store.MessageKindAgentTool, TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if got := unreadFor(owner.ID); got != base {
		t.Fatalf("activity messages bumped unread: expected %d, got %d", base, got)
	}

	// An ordinary message from the same member DOES bump unread.
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: other.ID, Body: "real message"}); err != nil {
		t.Fatal(err)
	}
	if got := unreadFor(owner.ID); got != base+1 {
		t.Fatalf("ordinary message did not bump unread by 1: expected %d, got %d", base+1, got)
	}
}

// TestAgentActivityMessagesExcludedFromSearch verifies the FTS trigger only
// indexes kind='message' rows, so activity bodies never surface in search.
func TestAgentActivityMessagesExcludedFromSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
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
	channel := channels[0]

	ordinary, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "zebrafish ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "zebrafish commentary", Kind: store.MessageKindAgentCommentary, TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}

	results, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Query:       "zebrafish",
		Limit:       50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Results) != 1 {
		t.Fatalf("expected exactly one search hit (the ordinary message), got %d: %#v", len(results.Results), results)
	}
	if results.Results[0].ID != ordinary.ID {
		t.Fatalf("search returned an unexpected (likely activity) row: %#v", results.Results[0])
	}
}
