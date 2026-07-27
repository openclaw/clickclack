package sqlite

import (
	"bytes"
	"context"
	"testing"

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
	if err := st.ReleaseGitHubDelivery(ctx, project.ID, "delivery-1"); err != nil {
		t.Fatal(err)
	}
	claim, err = st.ClaimGitHubDelivery(ctx, project.ID, "delivery-1", "pull_request")
	if err != nil || claim != store.GitHubDeliveryClaimed {
		t.Fatalf("expected released delivery to be claimable, claim=%q err=%v", claim, err)
	}
	if err := st.CompleteGitHubDelivery(ctx, project.ID, "delivery-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseGitHubDelivery(ctx, project.ID, "delivery-1"); err != nil {
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
