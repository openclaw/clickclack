package postgres

import (
	"context"
	"errors"
	"sync"
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

func TestManagedChannelReconciliationConvergesPostgres(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Reconcile Owner", Email: "reconcile-postgres@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Reconcile Postgres", Slug: "reconcile-postgres"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := store.ReconcileManagedChannelInput{
		WorkspaceID:      workspace.ID,
		UserID:           owner.ID,
		ExternalProvider: "github",
		ExternalRef:      "repo:42:pull:125",
		Name:             "pr-125",
		ExternalURL:      "https://github.com/openclaw/clickclack/pull/125",
		SidebarSection:   "Pull requests",
	}
	const callers = 6
	results := make(chan store.ReconcileManagedChannelResult, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := st.ReconcileManagedChannel(ctx, input)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var channelID string
	created := 0
	unchanged := 0
	for result := range results {
		if channelID == "" {
			channelID = result.Channel.ID
		}
		if result.Channel.ID != channelID {
			t.Fatalf("reconcile produced multiple channels: %s and %s", channelID, result.Channel.ID)
		}
		switch result.Action {
		case store.ManagedChannelActionCreated:
			created++
		case store.ManagedChannelActionUnchanged:
			unchanged++
		default:
			t.Fatalf("unexpected reconcile action: %#v", result)
		}
	}
	if created != 1 || unchanged != callers-1 {
		t.Fatalf("expected one create and %d unchanged results, got create=%d unchanged=%d", callers-1, created, unchanged)
	}
	changed := input
	changed.Archived = true
	changed.Name = "pr-125-review"
	updated, err := st.ReconcileManagedChannel(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Channel.ID != channelID || updated.Channel.ArchivedAt == nil || updated.Action != store.ManagedChannelActionUpdated {
		t.Fatalf("unexpected changed reconciliation: %#v", updated)
	}
}

func TestManagedChannelReconcileRetriesConcurrentNameConflictPostgres(t *testing.T) {
	t.Parallel()
	if !isManagedChannelReconcileConflict(errors.New(`duplicate key value violates unique constraint "channels_workspace_id_name_key"`)) {
		t.Fatal("expected concurrent channel-name collision to be retried")
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

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := storedb.New(tx).LockWorkspaceForUpdate(ctx, workspace.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := st.CreateUpload(ctx, store.CreateUploadInput{
			WorkspaceID: workspace.ID,
			OwnerID:     owner.ID,
			Filename:    "racing.txt",
			ContentType: "text/plain",
			ByteSize:    1,
			StoragePath: "memory://racing.txt",
		})
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
