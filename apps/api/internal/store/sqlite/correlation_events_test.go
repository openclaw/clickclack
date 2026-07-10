package sqlite

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/requestmeta"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestDurableMessageEventsPreserveOptionalCorrelationMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "correlation-owner@example.com")
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

	messageCtx := requestmeta.WithCorrelationID(ctx, "corr-sqlite-message")
	root, messageEvent, err := st.CreateMessage(messageCtx, store.CreateMessageInput{
		ChannelID: channels[0].ID,
		AuthorID:  owner.ID,
		Body:      "message content stays in the message row",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEventPayloadValue(t, messageEvent, "correlation_id", "corr-sqlite-message")
	assertEventPayloadMissing(t, messageEvent, "body")

	replyCtx := requestmeta.WithCorrelationID(ctx, "corr-sqlite-reply")
	_, _, replyEvents, err := st.CreateThreadReply(replyCtx, store.CreateThreadReplyInput{
		RootMessageID: root.ID,
		AuthorID:      owner.ID,
		Body:          "reply content stays in the message row",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replyEvents) != 2 || replyEvents[0].Type != "thread.reply_created" || replyEvents[1].Type != "thread.state_updated" {
		t.Fatalf("unexpected reply events: %#v", replyEvents)
	}
	assertEventPayloadValue(t, replyEvents[0], "correlation_id", "corr-sqlite-reply")
	assertEventPayloadMissing(t, replyEvents[0], "body")
	assertEventPayloadMissing(t, replyEvents[1], "correlation_id")

	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "correlation-second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, second.ID, "member"); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{second.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dmCtx := requestmeta.WithCorrelationID(ctx, "corr-sqlite-dm")
	_, dmEvent, err := st.CreateDirectMessage(dmCtx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "private content stays in the message row",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEventPayloadValue(t, dmEvent, "correlation_id", "corr-sqlite-dm")
	assertEventPayloadMissing(t, dmEvent, "body")

	_, uncorrelatedEvent, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID,
		AuthorID:  owner.ID,
		Body:      "backward-compatible event",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEventPayloadMissing(t, uncorrelatedEvent, "correlation_id")

	persisted, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]store.Event, len(persisted))
	for _, event := range persisted {
		byID[event.ID] = event
	}
	assertEventPayloadValue(t, byID[messageEvent.ID], "correlation_id", "corr-sqlite-message")
	assertEventPayloadValue(t, byID[replyEvents[0].ID], "correlation_id", "corr-sqlite-reply")
	assertEventPayloadValue(t, byID[dmEvent.ID], "correlation_id", "corr-sqlite-dm")
	assertEventPayloadMissing(t, byID[uncorrelatedEvent.ID], "correlation_id")
}

func assertEventPayloadValue(t *testing.T, event store.Event, key, want string) {
	t.Helper()
	got, ok := eventPayloadValue(event, key)
	if !ok || got != want {
		t.Fatalf("event %s payload[%q] = %q, %v; want %q", event.ID, key, got, ok, want)
	}
}

func assertEventPayloadMissing(t *testing.T, event store.Event, key string) {
	t.Helper()
	if got, ok := eventPayloadValue(event, key); ok {
		t.Fatalf("event %s unexpectedly has payload[%q] = %q", event.ID, key, got)
	}
}

func eventPayloadValue(event store.Event, key string) (string, bool) {
	switch payload := event.Payload.(type) {
	case map[string]string:
		value, ok := payload[key]
		return value, ok
	case map[string]any:
		value, ok := payload[key].(string)
		return value, ok
	default:
		return "", false
	}
}
