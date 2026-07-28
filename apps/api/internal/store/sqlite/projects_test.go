package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestProjectRoomLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "project-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Reviewer", Email: "project-reviewer@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}

	project, event, err := st.CreateProject(ctx, store.CreateProjectInput{
		WorkspaceID:   workspace.ID,
		Name:          "ClickClack",
		Description:   "Human and agent collaboration",
		CreatedBy:     owner.ID,
		WebhookSecret: "test-secret",
		Repositories: []store.CreateProjectRepositoryInput{
			{Owner: "openclaw", Name: "clickclack", FullName: "openclaw/clickclack", URL: "https://github.com/openclaw/clickclack"},
			{Owner: "block", Name: "buzz", FullName: "block/buzz", URL: "https://github.com/block/buzz"},
		},
		MemberIDs: []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "channel.created" || event.ChannelID != project.Channel.ID {
		t.Fatalf("unexpected project channel event: %#v", event)
	}
	if project.Channel.Name != "clickclack" || project.Channel.SidebarSection == nil || *project.Channel.SidebarSection != "Projects" {
		t.Fatalf("unexpected project channel: %#v", project.Channel)
	}
	if len(project.Repositories) != 2 || len(project.Members) != 2 {
		t.Fatalf("unexpected project context: %#v", project)
	}
	integrationUser, err := st.GetUser(ctx, project.IntegrationUserID)
	if err != nil {
		t.Fatal(err)
	}
	if integrationUser.Kind != "bot" || integrationUser.DisplayName != "GitHub" {
		t.Fatalf("unexpected project integration user: %#v", integrationUser)
	}
	secondProject, _, err := st.CreateProject(ctx, store.CreateProjectInput{
		WorkspaceID:   workspace.ID,
		Name:          "Buzz",
		CreatedBy:     owner.ID,
		WebhookSecret: "second-test-secret",
		Repositories: []store.CreateProjectRepositoryInput{{
			Owner: "block", Name: "buzz", FullName: "block/buzz", URL: "https://github.com/block/buzz",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondProject.IntegrationUserID != project.IntegrationUserID {
		t.Fatalf("projects use different GitHub users: %q and %q", project.IntegrationUserID, secondProject.IntegrationUserID)
	}
	var githubMemberCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = ?
		  AND u.kind = 'bot'
		  AND u.display_name = 'GitHub'
	`, workspace.ID).Scan(&githubMemberCount); err != nil {
		t.Fatal(err)
	}
	if githubMemberCount != 1 {
		t.Fatalf("GitHub workspace member count = %d, want 1", githubMemberCount)
	}

	contextProject, err := st.GetProject(ctx, project.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if contextProject.Description != "Human and agent collaboration" {
		t.Fatalf("unexpected project read: %#v", contextProject)
	}
	target, err := st.GetGitHubWebhookTarget(ctx, project.ID, "OPENCLAW/CLICKCLACK")
	if err != nil {
		t.Fatal(err)
	}
	if target.ChannelID != project.Channel.ID || target.WebhookSecret != "test-secret" {
		t.Fatalf("unexpected webhook target: %#v", target)
	}
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
		`SELECT status FROM github_deliveries WHERE project_id = ? AND delivery_id = ?`,
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
		`UPDATE github_deliveries SET updated_at = ? WHERE project_id = ? AND delivery_id = ?`,
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
	var exported bytes.Buffer
	if err := st.ExportJSON(ctx, &exported); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exported.Bytes(), []byte(`"projects"`)) || bytes.Contains(exported.Bytes(), []byte("test-secret")) {
		t.Fatalf("project export missing metadata or leaked webhook secret: %s", exported.String())
	}
}

func TestGitHubDeliveryRetryMigrationUpgradesExistingRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "project-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0040_github_delivery_retries.sql")

	owner, err := st.EnsureBootstrap(ctx, "Upgrade Owner", "project-upgrade@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: seedSQLiteLegacyProject(t, ctx, st, workspaces[0].ID, owner.ID, "Upgrade")}
	const processingAt = "2026-01-02T03:04:05Z"
	const completedAt = "2026-01-02T04:05:06Z"
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO github_deliveries (project_id, delivery_id, event_type, status, created_at, completed_at)
		VALUES (?, 'old-processing', 'issues', 'processing', ?, NULL),
		       (?, 'old-complete', 'pull_request', 'complete', ?, ?)
	`, project.ID, processingAt, project.ID, processingAt, completedAt); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		deliveryID, wantStatus, wantUpdated string
		wantFailed                          bool
	}{
		{deliveryID: "old-processing", wantStatus: "processing", wantUpdated: processingAt},
		{deliveryID: "old-complete", wantStatus: "complete", wantUpdated: completedAt},
	} {
		var status, updatedAt string
		var failedAt sql.NullString
		if err := st.db.QueryRowContext(ctx, `
			SELECT status, updated_at, failed_at
			FROM github_deliveries
			WHERE project_id = ? AND delivery_id = ?
		`, project.ID, tc.deliveryID).Scan(&status, &updatedAt, &failedAt); err != nil {
			t.Fatal(err)
		}
		if status != tc.wantStatus || updatedAt != tc.wantUpdated || failedAt.Valid != tc.wantFailed {
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

func TestWorkspaceProjectIntegrationMigrationDeduplicatesGitHubBots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "project-integration-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0042_workspace_project_integrations.sql")

	owner, err := st.EnsureBootstrap(ctx, "Upgrade Owner", "project-integration-upgrade@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces[0].ID
	firstProjectID := seedSQLiteLegacyProject(t, ctx, st, workspaceID, owner.ID, "First")
	secondProjectID := seedSQLiteLegacyProject(t, ctx, st, workspaceID, owner.ID, "Second")
	unrelatedUserID := newID("usr")
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO users (id, display_name, avatar_url, created_at, handle, kind, owner_user_id)
		VALUES (?, 'GitHub', '', ?, '', 'bot', NULL)
	`, unrelatedUserID, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES (?, ?, 'bot', ?)
	`, workspaceID, unrelatedUserID, now()); err != nil {
		t.Fatal(err)
	}

	var firstUserID string
	if err := st.db.QueryRowContext(ctx, `
		SELECT integration_user_id FROM projects
		WHERE workspace_id = ?
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
		if err := st.db.QueryRowContext(ctx, `SELECT integration_user_id FROM projects WHERE id = ?`, projectID).Scan(&integrationUserID); err != nil {
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
		WHERE wm.workspace_id = ? AND u.kind = 'bot' AND u.display_name = 'GitHub'
	`, workspaceID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if memberCount != 2 {
		t.Fatalf("GitHub workspace member count after migration = %d, want canonical and unrelated bots", memberCount)
	}
	var unrelatedMemberCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND user_id = ?
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

func seedSQLiteLegacyProject(t *testing.T, ctx context.Context, st *Store, workspaceID, ownerID, name string) string {
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
		VALUES (?, 'GitHub', '', ?, '', 'bot', NULL)
	`, integrationUserID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES (?, ?, 'bot', ?)
	`, workspaceID, integrationUserID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO channels (id, workspace_id, name, kind, created_at)
		VALUES (?, ?, ?, 'public', ?)
	`, channelID, workspaceID, slug(name), createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO projects (
			id, workspace_id, name, slug, description, channel_id, integration_user_id,
			webhook_secret, created_by, created_at
		)
		VALUES (?, ?, ?, ?, '', ?, ?, 'test-secret', ?, ?)
	`, projectID, workspaceID, name, slug(name), channelID, integrationUserID, ownerID, createdAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return projectID
}

func TestProjectCreationRequiresWorkspaceManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "project-manager-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "project-manager-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateProject(ctx, store.CreateProjectInput{
		WorkspaceID:   workspaces[0].ID,
		Name:          "Denied",
		CreatedBy:     member.ID,
		WebhookSecret: "test-secret",
		Repositories: []store.CreateProjectRepositoryInput{
			{Owner: "block", Name: "buzz", FullName: "block/buzz", URL: "https://github.com/block/buzz"},
		},
	}); err == nil {
		t.Fatal("expected non-manager project creation to be rejected")
	}
	projects, err := st.ListProjects(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected rejected creation to roll back, got %#v", projects)
	}
}
