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
	project, _, err := st.CreateProject(ctx, store.CreateProjectInput{
		WorkspaceID: workspaces[0].ID, Name: "Upgrade", CreatedBy: owner.ID, WebhookSecret: "test-secret",
		Repositories: []store.CreateProjectRepositoryInput{{
			Owner: "openclaw", Name: "clickclack", FullName: "openclaw/clickclack",
			URL: "https://github.com/openclaw/clickclack",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
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
