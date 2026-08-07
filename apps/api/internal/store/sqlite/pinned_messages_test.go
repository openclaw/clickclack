package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func TestPinnedMessagesMigrationUpgradesExistingDatabase(t *testing.T) {
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "pins-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0040_pinned_messages.sql")
	// Message read-back includes the T2 cognitive columns; apply that
	// independent schema addition early while leaving the pins migration
	// under test unapplied, and record it so Migrate does not run it twice.
	applySQLiteMigrations(t, ctx, st, "0041_cognitive_os_message_fields.sql")
	if _, err := st.db.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, "0041_cognitive_os_message_fields.sql", now()); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Pin Upgrade", "pin-upgrade@example.com")
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
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "existing message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PinMessage(ctx, channels[0].ID, message.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	pinned, err := st.ListPinnedMessages(ctx, channels[0].ID, owner.ID, 100)
	if err != nil || len(pinned) != 1 || pinned[0].ID != message.ID {
		t.Fatalf("unexpected upgraded pin list: %#v: %v", pinned, err)
	}
	if _, err := st.UnpinMessage(ctx, channels[0].ID, message.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedMessageLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "pin-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("expected a channel, got %v: %v", channels, err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "keep this",
	})
	if err != nil {
		t.Fatal(err)
	}
	pin, added, err := st.PinMessage(ctx, channels[0].ID, message.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.MessageID != message.ID || added.Type != "pin.added" {
		t.Fatalf("unexpected pin result: %#v %#v", pin, added)
	}
	if _, _, err := st.PinMessage(ctx, channels[0].ID, message.ID, owner.ID); err != store.ErrAlreadyPinned {
		t.Fatalf("expected ErrAlreadyPinned, got %v", err)
	}
	pinned, err := st.ListPinnedMessages(ctx, channels[0].ID, owner.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 1 || pinned[0].ID != message.ID {
		t.Fatalf("unexpected pinned messages: %#v", pinned)
	}
	removed, err := st.UnpinMessage(ctx, channels[0].ID, message.ID, owner.ID)
	if err != nil || removed.Type != "pin.removed" {
		t.Fatalf("unexpected unpin result: %#v %v", removed, err)
	}
	if _, err := st.UnpinMessage(ctx, channels[0].ID, message.ID, owner.ID); err != store.ErrPinnedMessageNotFound {
		t.Fatalf("expected ErrPinnedMessageNotFound, got %v", err)
	}
}

func TestDeletePinnedMessageRemovesPinAndOrdersEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "pin-delete-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("expected a channel, got %v: %v", channels, err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "delete this pin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PinMessage(ctx, channels[0].ID, message.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	deleted, events, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedAt == nil || len(events) != 2 || events[0].Type != "pin.removed" || events[1].Type != "message.deleted" {
		t.Fatalf("unexpected delete result: %#v %#v", deleted, events)
	}
	count, err := st.q.CountPinnedMessage(ctx, storedb.CountPinnedMessageParams{
		ChannelID: channels[0].ID,
		MessageID: message.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected deleted message pin to be removed, got %d rows", count)
	}
}

func TestPinnedMessageLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "pin-limit@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("expected a channel, got %v: %v", channels, err)
	}
	for index := 0; index <= store.MaxPinnedMessagesPerChannel; index++ {
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channels[0].ID,
			AuthorID:  owner.ID,
			Body:      fmt.Sprintf("pin %d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = st.PinMessage(ctx, channels[0].ID, message.ID, owner.ID)
		if index < store.MaxPinnedMessagesPerChannel && err != nil {
			t.Fatalf("pin %d failed: %v", index, err)
		}
		if index == store.MaxPinnedMessagesPerChannel && err != store.ErrPinnedMessageLimit {
			t.Fatalf("expected pin limit, got %v", err)
		}
	}
}

func TestPinnedMessageLimitIsAtomic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "pin-atomic@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("expected a channel, got %v: %v", channels, err)
	}
	channelID := channels[0].ID
	for index := 0; index < store.MaxPinnedMessagesPerChannel-1; index++ {
		message, _, createErr := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channelID, AuthorID: owner.ID, Body: fmt.Sprintf("seed pin %d", index)})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, _, pinErr := st.PinMessage(ctx, channelID, message.ID, owner.ID); pinErr != nil {
			t.Fatal(pinErr)
		}
	}
	candidates := make([]store.Message, 2)
	for index := range candidates {
		candidates[index], _, err = st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channelID, AuthorID: owner.ID, Body: fmt.Sprintf("candidate %d", index)})
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, len(candidates))
	var ready sync.WaitGroup
	ready.Add(len(candidates))
	for _, message := range candidates {
		go func(messageID string) {
			ready.Done()
			<-start
			_, _, pinErr := st.PinMessage(ctx, channelID, messageID, owner.ID)
			results <- pinErr
		}(message.ID)
	}
	ready.Wait()
	close(start)
	var succeeded, limited int
	for range candidates {
		switch result := <-results; result {
		case nil:
			succeeded++
		case store.ErrPinnedMessageLimit:
			limited++
		default:
			t.Fatalf("unexpected concurrent pin result: %v", result)
		}
	}
	if succeeded != 1 || limited != 1 {
		t.Fatalf("expected one success and one limit error, got success=%d limited=%d", succeeded, limited)
	}
	pinned, err := st.ListPinnedMessages(ctx, channelID, owner.ID, store.MaxPinnedMessagesPerChannel)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != store.MaxPinnedMessagesPerChannel {
		t.Fatalf("expected %d pins, got %d", store.MaxPinnedMessagesPerChannel, len(pinned))
	}
}
