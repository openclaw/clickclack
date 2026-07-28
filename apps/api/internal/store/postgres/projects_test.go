package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestProjectsShareWorkspaceGitHubIntegration(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Project Owner", "postgres-project-integration@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var projects []store.Project
	for _, name := range []string{"First", "Second"} {
		project, _, err := st.CreateProject(ctx, store.CreateProjectInput{
			WorkspaceID:   workspaces[0].ID,
			Name:          name,
			CreatedBy:     owner.ID,
			WebhookSecret: "test-secret",
			Repositories: []store.CreateProjectRepositoryInput{{
				Owner: "openclaw", Name: slug(name), FullName: "openclaw/" + slug(name),
				URL: "https://github.com/openclaw/" + slug(name),
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		projects = append(projects, project)
	}
	if projects[0].IntegrationUserID != projects[1].IntegrationUserID {
		t.Fatalf("projects use different GitHub users: %q and %q", projects[0].IntegrationUserID, projects[1].IntegrationUserID)
	}
	var githubMemberCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1
		  AND u.kind = 'bot'
		  AND u.display_name = 'GitHub'
	`, workspaces[0].ID).Scan(&githubMemberCount); err != nil {
		t.Fatal(err)
	}
	if githubMemberCount != 1 {
		t.Fatalf("GitHub workspace member count = %d, want 1", githubMemberCount)
	}
}

func TestGitHubDeliveryRetryLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	project := createPostgresDeliveryTestProject(t, ctx, st, "postgres-project-retries@example.com")

	claim, err := st.ClaimGitHubDelivery(ctx, project.ID, "delivery-1", "pull_request")
	if err != nil || claim != store.GitHubDeliveryClaimed {
		t.Fatalf("expected first delivery claim, claim=%q err=%v", claim, err)
	}
	claim, err = st.ClaimGitHubDelivery(ctx, project.ID, "delivery-1", "pull_request")
	if err != nil || claim != store.GitHubDeliveryProcessing {
		t.Fatalf("expected processing delivery state, claim=%q err=%v", claim, err)
	}
	if err := st.FailGitHubDelivery(ctx, project.ID, "delivery-1"); err != nil {
		t.Fatal(err)
	}
	var failedStatus string
	if err := st.db.QueryRowContext(ctx,
		`SELECT status FROM github_deliveries WHERE project_id = $1 AND delivery_id = $2`,
		project.ID, "delivery-1",
	).Scan(&failedStatus); err != nil {
		t.Fatal(err)
	}
	if failedStatus != "failed" {
		t.Fatalf("expected failed delivery state, got %q", failedStatus)
	}
	claim, err = st.ClaimGitHubDelivery(ctx, project.ID, "delivery-1", "pull_request")
	if err != nil || claim != store.GitHubDeliveryClaimed {
		t.Fatalf("expected failed delivery to be claimable, claim=%q err=%v", claim, err)
	}
	staleAt := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	if _, err := st.db.ExecContext(ctx,
		`UPDATE github_deliveries SET updated_at = $1 WHERE project_id = $2 AND delivery_id = $3`,
		staleAt, project.ID, "delivery-1",
	); err != nil {
		t.Fatal(err)
	}
	claim, err = st.ClaimGitHubDelivery(ctx, project.ID, "delivery-1", "pull_request")
	if err != nil || claim != store.GitHubDeliveryClaimed {
		t.Fatalf("expected stale processing delivery to be claimable, claim=%q err=%v", claim, err)
	}
	if err := st.CompleteGitHubDelivery(ctx, project.ID, "delivery-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.FailGitHubDelivery(ctx, project.ID, "delivery-1"); err != nil {
		t.Fatal(err)
	}
	claim, err = st.ClaimGitHubDelivery(ctx, project.ID, "delivery-1", "pull_request")
	if err != nil || claim != store.GitHubDeliveryComplete {
		t.Fatalf("expected completed delivery state, claim=%q err=%v", claim, err)
	}
}

func TestGitHubDeliveryRetryMigrationUpgradesExistingRows(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	applyPostgresMigrationsBefore(t, ctx, st, "0033_github_delivery_retries.sql")
	owner, err := st.EnsureBootstrap(ctx, "Project Owner", "postgres-project-upgrade@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: seedPostgresLegacyProject(t, ctx, st, workspaces[0].ID, owner.ID, "Delivery upgrade")}

	const processingAt = "2026-01-02T03:04:05Z"
	const completedAt = "2026-01-02T04:05:06Z"
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO github_deliveries (project_id, delivery_id, event_type, status, created_at, completed_at)
		VALUES ($1, 'old-processing', 'issues', 'processing', $2, NULL),
		       ($1, 'old-complete', 'pull_request', 'complete', $2, $3)
	`, project.ID, processingAt, completedAt); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		deliveryID, wantStatus, wantUpdated string
	}{
		{deliveryID: "old-processing", wantStatus: "processing", wantUpdated: processingAt},
		{deliveryID: "old-complete", wantStatus: "complete", wantUpdated: completedAt},
	} {
		var status, updatedAt string
		var failedAt sql.NullString
		if err := st.db.QueryRowContext(ctx, `
			SELECT status, updated_at, failed_at
			FROM github_deliveries
			WHERE project_id = $1 AND delivery_id = $2
		`, project.ID, tc.deliveryID).Scan(&status, &updatedAt, &failedAt); err != nil {
			t.Fatal(err)
		}
		if status != tc.wantStatus || updatedAt != tc.wantUpdated || failedAt.Valid {
			t.Fatalf("%s upgrade state = %q, %q, %#v", tc.deliveryID, status, updatedAt, failedAt)
		}
	}

	if err := st.FailGitHubDelivery(ctx, project.ID, "old-processing"); err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimGitHubDelivery(ctx, project.ID, "old-processing", "issues")
	if err != nil || claim != store.GitHubDeliveryClaimed {
		t.Fatalf("expected upgraded failed delivery to be retryable, claim=%q err=%v", claim, err)
	}
	claim, err = st.ClaimGitHubDelivery(ctx, project.ID, "old-complete", "pull_request")
	if err != nil || claim != store.GitHubDeliveryComplete {
		t.Fatalf("expected upgraded complete delivery to stay deduplicated, claim=%q err=%v", claim, err)
	}
}

func TestWorkspaceProjectIntegrationMigrationDeduplicatesPostgresGitHubBots(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	applyPostgresMigrationsBefore(t, ctx, st, "0035_workspace_project_integrations.sql")
	owner, err := st.EnsureBootstrap(ctx, "Project Owner", "postgres-project-integration-upgrade@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces[0].ID
	firstProjectID := seedPostgresLegacyProject(t, ctx, st, workspaceID, owner.ID, "First")
	secondProjectID := seedPostgresLegacyProject(t, ctx, st, workspaceID, owner.ID, "Second")
	unrelatedUserID := newID("usr")
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO users (id, display_name, avatar_url, created_at, handle, kind, owner_user_id)
		VALUES ($1, 'GitHub', '', $2, '', 'bot', NULL)
	`, unrelatedUserID, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES ($1, $2, 'bot', $3)
	`, workspaceID, unrelatedUserID, now()); err != nil {
		t.Fatal(err)
	}

	var firstUserID string
	if err := st.db.QueryRowContext(ctx, `
		SELECT integration_user_id FROM projects
		WHERE workspace_id = $1
		ORDER BY created_at, id
		LIMIT 1
	`, workspaceID).Scan(&firstUserID); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, projectID := range []string{firstProjectID, secondProjectID} {
		var integrationUserID string
		if err := st.db.QueryRowContext(ctx, `SELECT integration_user_id FROM projects WHERE id = $1`, projectID).Scan(&integrationUserID); err != nil {
			t.Fatal(err)
		}
		if integrationUserID != firstUserID {
			t.Fatalf("project %s integration user = %q, want %q", projectID, integrationUserID, firstUserID)
		}
	}
	var memberCount, userCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1 AND u.kind = 'bot' AND u.display_name = 'GitHub'
	`, workspaceID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if memberCount != 2 {
		t.Fatalf("GitHub workspace member count after migration = %d, want canonical and unrelated bots", memberCount)
	}
	var unrelatedMemberCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workspace_members WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, unrelatedUserID).Scan(&unrelatedMemberCount); err != nil {
		t.Fatal(err)
	}
	if unrelatedMemberCount != 1 {
		t.Fatal("migration removed an unrelated GitHub-named bot")
	}
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users WHERE kind = 'bot' AND display_name = 'GitHub'
	`).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 3 {
		t.Fatalf("GitHub user count after migration = %d, want historical and unrelated users", userCount)
	}
}

func seedPostgresLegacyProject(t *testing.T, ctx context.Context, st *Store, workspaceID, ownerID, name string) string {
	t.Helper()
	createdAt := now()
	integrationUserID := newID("usr")
	channelID := newID("chn")
	projectID := newID("prj")
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, display_name, avatar_url, created_at, handle, kind, owner_user_id)
		VALUES ($1, 'GitHub', '', $2, '', 'bot', NULL)
	`, integrationUserID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES ($1, $2, 'bot', $3)
	`, workspaceID, integrationUserID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO channels (id, workspace_id, name, kind, created_at)
		VALUES ($1, $2, $3, 'public', $4)
	`, channelID, workspaceID, slug(name), createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO projects (
			id, workspace_id, name, slug, description, channel_id, integration_user_id,
			webhook_secret, created_by, created_at
		)
		VALUES ($1, $2, $3, $4, '', $5, $6, 'test-secret', $7, $8)
	`, projectID, workspaceID, name, slug(name), channelID, integrationUserID, ownerID, createdAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return projectID
}

func createPostgresDeliveryTestProject(t *testing.T, ctx context.Context, st *Store, email string) store.Project {
	t.Helper()
	owner, err := st.EnsureBootstrap(ctx, "Project Owner", email)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := st.CreateProject(ctx, store.CreateProjectInput{
		WorkspaceID: workspaces[0].ID, Name: "Delivery retries", CreatedBy: owner.ID, WebhookSecret: "test-secret",
		Repositories: []store.CreateProjectRepositoryInput{{
			Owner: "openclaw", Name: "clickclack", FullName: "openclaw/clickclack",
			URL: "https://github.com/openclaw/clickclack",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return project
}
