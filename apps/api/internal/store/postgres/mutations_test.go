package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func TestManagedChannelFieldsRoundTripPostgres(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Managed Owner", Email: "managed-postgres@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Managed Postgres", Slug: "managed-postgres"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID:     workspace.ID,
		UserID:          owner.ID,
		Name:            "managed-session",
		ExternalManaged: true,
		ExternalRef:     "session:postgres",
		ExternalURL:     "https://control.example.com/sessions/postgres",
		SidebarSection:  "Sessions",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !channel.ExternalManaged || channel.ExternalRef == nil || channel.ExternalURL == nil || channel.SidebarSection == nil {
		t.Fatalf("unexpected created managed channel: %#v", channel)
	}
	clear := ""
	archived := true
	updated, event, err := st.UpdateChannel(ctx, store.UpdateChannelInput{
		ChannelID:      channel.ID,
		UserID:         owner.ID,
		Archived:       &archived,
		ExternalRef:    &clear,
		ExternalURL:    &clear,
		SidebarSection: &clear,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ExternalRef != nil || updated.ExternalURL != nil || updated.SidebarSection != nil || updated.ArchivedAt == nil {
		t.Fatalf("managed fields were not cleared: %#v", updated)
	}
	payload, ok := event.Payload.(map[string]any)
	if !ok || payload["archived"] != true {
		t.Fatalf("channel.updated archive metadata missing: %#v", event.Payload)
	}
}

func TestChannelDisplayTitleRoundTripPostgres(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Display Owner", Email: "display-postgres@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Display Postgres", Slug: "display-postgres"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID:  workspace.ID,
		UserID:       owner.ID,
		Name:         "display-session",
		DisplayTitle: "  " + strings.Repeat("界", 205) + "  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if channel.DisplayTitle == nil || len([]rune(*channel.DisplayTitle)) != 200 {
		t.Fatalf("display title was not rune-truncated: %#v", channel.DisplayTitle)
	}
	listed, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range listed {
		if candidate.ID == channel.ID {
			found = candidate.DisplayTitle != nil && *candidate.DisplayTitle == *channel.DisplayTitle
		}
	}
	got, err := st.GetChannel(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.DisplayTitle == nil || *got.DisplayTitle != *channel.DisplayTitle {
		t.Fatalf("display title did not roundtrip: list=%#v get=%#v", listed, got)
	}
	trailingBoundary := strings.Repeat("界", 199) + "  X"
	updated, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channel.ID, UserID: owner.ID, DisplayTitle: &trailingBoundary})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayTitle == nil || len([]rune(*updated.DisplayTitle)) != 199 || strings.HasSuffix(*updated.DisplayTitle, " ") {
		t.Fatalf("truncated display title retained boundary whitespace: %#v", updated.DisplayTitle)
	}
	next := "  Sensible Work Tree Naming Scheme  "
	updated, _, err = st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channel.ID, UserID: owner.ID, DisplayTitle: &next})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayTitle == nil || *updated.DisplayTitle != "Sensible Work Tree Naming Scheme" {
		t.Fatalf("display title was not updated: %#v", updated.DisplayTitle)
	}
	clear := ""
	updated, _, err = st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channel.ID, UserID: owner.ID, DisplayTitle: &clear})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayTitle != nil {
		t.Fatalf("display title was not cleared: %#v", updated.DisplayTitle)
	}
}

func TestChannelUpdateSerializesPartialWrites(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Channel Owner", "channel-lock@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "before-lock"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx := mustBeginPostgresTx(t, ctx, st.db)
	if _, err := tx.ExecContext(ctx, `UPDATE channels SET name = 'concurrent-name', archived_at = '2026-08-30T00:00:00Z', external_managed = 1, external_ref = 'concurrent-ref' WHERE id = $1`, channel.ID); err != nil {
		t.Fatal(err)
	}
	nextTitle := "New display title"
	type updateResult struct {
		channel store.Channel
		event   store.Event
		err     error
	}
	result := make(chan updateResult, 1)
	go func() {
		updated, event, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channel.ID, UserID: owner.ID, DisplayTitle: &nextTitle})
		result <- updateResult{updated, event, err}
	}()
	waitForBlockedPostgresQuery(t, ctx, st.db, "channels")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	updated := <-result
	if updated.err != nil {
		t.Fatal(updated.err)
	}
	persisted, err := st.GetChannel(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []store.Channel{updated.channel, persisted} {
		if got.Name != "concurrent-name" || got.DisplayTitle == nil || *got.DisplayTitle != nextTitle || got.ArchivedAt == nil || !got.ExternalManaged || got.ExternalRef == nil || *got.ExternalRef != "concurrent-ref" {
			t.Fatalf("partial channel update lost a concurrent field: %#v", got)
		}
	}
	payload, ok := updated.event.Payload.(map[string]any)
	if !ok || payload["archived"] != true {
		t.Fatalf("channel.updated lost concurrent archive state: %#v", updated.event.Payload)
	}
}

func TestWorkspaceUpdateSerializesPartialWrites(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "update-lock@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Before", Slug: "before-lock"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	qtx := st.q.WithTx(tx)
	if err := qtx.LockWorkspaceForUpdate(ctx, workspace.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET name = 'Concurrent name' WHERE id = $1`, workspace.ID); err != nil {
		t.Fatal(err)
	}
	nextSlug := "after-lock"
	result := make(chan error, 1)
	go func() {
		_, _, err := st.UpdateWorkspace(ctx, store.UpdateWorkspaceInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, Slug: &nextSlug})
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("workspace update bypassed row lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	updated, err := st.GetWorkspace(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Concurrent name" || updated.Slug != nextSlug {
		t.Fatalf("partial update lost concurrent field: %#v", updated)
	}
}

func TestWorkspaceDeleteLockBlocksNewUploads(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "delete-lock@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Delete Lock", Slug: "delete-lock"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "racing.txt",
		ContentType: "text/plain",
		ByteSize:    1,
		StoragePath: "memory://racing.txt",
	}
	reservation, err := st.ReserveUploadQuota(ctx, workspace.ID, owner.ID, "", input.ByteSize)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := storedb.New(tx).LockWorkspaceForUpdate(ctx, workspace.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := st.CreateReservedUpload(ctx, reservation.ID, input)
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("upload insert bypassed workspace deletion lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDeleteMessagePreservesDirectMessageBoundary(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "pg-dm-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "PG DM Delete", Slug: "pg-dm-delete"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "pg-dm-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "pg-dm-other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []store.User{member, other} {
		if err := st.AddWorkspaceMember(ctx, workspace.ID, user.ID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
	}

	ownerDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	memberMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: ownerDM.ID, AuthorID: member.ID, Body: "owner is participant but not author"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: memberMessage.ID, UserID: owner.ID}); !errors.Is(err, store.ErrMessageNotWritable) {
		t.Fatalf("expected owner non-author DM participant delete to be rejected, got %v", err)
	}
	deletedByAuthor, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: memberMessage.ID, UserID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deletedByAuthor.DeletedAt == nil {
		t.Fatalf("expected DM author delete to soft-delete message, got %#v", deletedByAuthor)
	}

	memberDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: member.ID, MemberIDs: []string{other.ID}})
	if err != nil {
		t.Fatal(err)
	}
	privateMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: memberDM.ID, AuthorID: member.ID, Body: "owner is outside this dm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: privateMessage.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected owner outside DM to be blocked from deleting the message")
	}
}
