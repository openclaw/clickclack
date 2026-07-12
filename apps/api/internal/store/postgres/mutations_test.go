package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

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
