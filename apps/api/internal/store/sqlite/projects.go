package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) ListProjects(ctx context.Context, workspaceID, userID string) ([]store.Project, error) {
	if err := s.requireMembership(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListProjects(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	projects := make([]store.Project, 0, len(rows))
	for _, row := range rows {
		project := projectFromListRow(row)
		if err := hydrateProject(ctx, s.q, &project); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, nil
}

func (s *Store) GetProject(ctx context.Context, projectID, userID string) (store.Project, error) {
	row, err := s.q.GetProject(ctx, projectID)
	if err != nil {
		return store.Project{}, err
	}
	if err := s.requireMembership(ctx, row.WorkspaceID, userID); err != nil {
		return store.Project{}, err
	}
	project := projectFromGetRow(row)
	if err := hydrateProject(ctx, s.q, &project); err != nil {
		return store.Project{}, err
	}
	return project, nil
}

func (s *Store) CreateProject(ctx context.Context, input store.CreateProjectInput) (store.Project, store.Event, error) {
	if err := store.ValidateCreateProjectInput(input); err != nil {
		return store.Project{}, store.Event{}, err
	}
	name := strings.TrimSpace(input.Name)
	description := strings.TrimSpace(input.Description)
	projectSlug := slug(input.Slug)
	if projectSlug == "" {
		projectSlug = slug(name)
	}
	if projectSlug == "" || len([]rune(projectSlug)) > 80 {
		return store.Project{}, store.Event{}, errors.New("project slug must be between 1 and 80 characters")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Project{}, store.Event{}, err
	}
	defer tx.Rollback()
	if err := requireWorkspaceManagerTx(ctx, tx, input.WorkspaceID, input.CreatedBy); err != nil {
		return store.Project{}, store.Event{}, err
	}
	qtx := s.q.WithTx(tx)
	createdAt := now()
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		projectID = newID("prj")
	}
	integrationUserID := newID("usr")
	channelID := newID("chn")

	if err := qtx.InsertBotUser(ctx, storedb.InsertBotUserParams{
		ID:          integrationUserID,
		OwnerUserID: sqlOptionalText(""),
		DisplayName: "GitHub",
		Handle:      "",
		AvatarUrl:   "",
		CreatedAt:   createdAt,
	}); err != nil {
		return store.Project{}, store.Event{}, err
	}
	if err := qtx.InsertWorkspaceMember(ctx, storedb.InsertWorkspaceMemberParams{
		WorkspaceID: input.WorkspaceID,
		UserID:      integrationUserID,
		Role:        "bot",
		CreatedAt:   createdAt,
	}); err != nil {
		return store.Project{}, store.Event{}, err
	}

	var channelRouteID string
	for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
		channelRouteID, err = newRouteID('C')
		if err != nil {
			return store.Project{}, store.Event{}, err
		}
		err = qtx.InsertChannel(ctx, storedb.InsertChannelParams{
			ID:              channelID,
			RouteID:         sqlText(channelRouteID),
			WorkspaceID:     input.WorkspaceID,
			Name:            projectSlug,
			Kind:            "public",
			CreatedAt:       createdAt,
			ExternalManaged: databaseBool(false),
			ExternalRef:     sqlOptionalText("github-project:" + projectID),
			ExternalUrl:     sqlOptionalText(input.Repositories[0].URL),
			SidebarSection:  sqlOptionalText("Projects"),
		})
		if err == nil {
			break
		}
		if !isRouteIDConflict(err) {
			return store.Project{}, store.Event{}, err
		}
	}
	if err != nil {
		return store.Project{}, store.Event{}, errors.New("could not create project channel route_id after collision retries")
	}

	if err := qtx.InsertProject(ctx, storedb.InsertProjectParams{
		ID:                projectID,
		WorkspaceID:       input.WorkspaceID,
		Name:              name,
		Slug:              projectSlug,
		Description:       description,
		ChannelID:         channelID,
		IntegrationUserID: integrationUserID,
		WebhookSecret:     strings.TrimSpace(input.WebhookSecret),
		CreatedBy:         input.CreatedBy,
		CreatedAt:         createdAt,
	}); err != nil {
		return store.Project{}, store.Event{}, err
	}
	for _, repository := range input.Repositories {
		if err := qtx.InsertProjectRepository(ctx, storedb.InsertProjectRepositoryParams{
			ID:        newID("rep"),
			ProjectID: projectID,
			Owner:     repository.Owner,
			Name:      repository.Name,
			FullName:  repository.FullName,
			Url:       repository.URL,
			CreatedAt: createdAt,
		}); err != nil {
			return store.Project{}, store.Event{}, err
		}
	}
	memberIDs := append([]string{input.CreatedBy}, input.MemberIDs...)
	seen := make(map[string]struct{}, len(memberIDs))
	for _, memberID := range memberIDs {
		memberID = strings.TrimSpace(memberID)
		if memberID == "" {
			continue
		}
		if _, ok := seen[memberID]; ok {
			continue
		}
		seen[memberID] = struct{}{}
		if err := requireMembershipTx(ctx, tx, input.WorkspaceID, memberID); err != nil {
			return store.Project{}, store.Event{}, errors.New("project participant is not a workspace member")
		}
		role := "member"
		if memberID == input.CreatedBy {
			role = "admin"
		}
		if err := qtx.InsertProjectMember(ctx, storedb.InsertProjectMemberParams{
			ProjectID: projectID,
			UserID:    memberID,
			Role:      role,
			CreatedAt: createdAt,
		}); err != nil {
			return store.Project{}, store.Event{}, err
		}
	}
	event, err := insertEvent(ctx, tx, input.WorkspaceID, channelID, "channel.created", nil, map[string]string{"channel_id": channelID})
	if err != nil {
		return store.Project{}, store.Event{}, err
	}
	row, err := qtx.GetProject(ctx, projectID)
	if err != nil {
		return store.Project{}, store.Event{}, err
	}
	project := projectFromGetRow(row)
	if err := hydrateProject(ctx, qtx, &project); err != nil {
		return store.Project{}, store.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Project{}, store.Event{}, err
	}
	return project, event, nil
}

func (s *Store) GetGitHubWebhookTarget(ctx context.Context, projectID, repositoryFullName string) (store.GitHubWebhookTarget, error) {
	row, err := s.q.GetGitHubWebhookTarget(ctx, storedb.GetGitHubWebhookTargetParams{
		ProjectID:          projectID,
		RepositoryFullName: strings.ToLower(strings.TrimSpace(repositoryFullName)),
	})
	if err != nil {
		return store.GitHubWebhookTarget{}, err
	}
	return store.GitHubWebhookTarget{
		ProjectID: row.ProjectID, WorkspaceID: row.WorkspaceID, ChannelID: row.ChannelID,
		IntegrationUserID: row.IntegrationUserID, RepositoryID: row.RepositoryID,
		RepositoryFullName: row.RepositoryFullName, WebhookSecret: row.WebhookSecret,
	}, nil
}

func (s *Store) ClaimGitHubDelivery(ctx context.Context, projectID, deliveryID, eventType string) (store.GitHubDeliveryClaim, error) {
	for attempt := 0; attempt < 3; attempt++ {
		claimedAt := now()
		rows, err := s.q.ClaimGitHubDelivery(ctx, storedb.ClaimGitHubDeliveryParams{
			ProjectID: projectID, DeliveryID: deliveryID, EventType: eventType,
			CreatedAt: claimedAt, UpdatedAt: claimedAt,
		})
		if err != nil {
			return "", err
		}
		if rows == 1 {
			return store.GitHubDeliveryClaimed, nil
		}
		rows, err = s.q.ReclaimRetryableGitHubDelivery(ctx, storedb.ReclaimRetryableGitHubDeliveryParams{
			EventType: eventType, UpdatedAt: claimedAt, ProjectID: projectID, DeliveryID: deliveryID,
			StaleBefore: time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano),
		})
		if err != nil {
			return "", err
		}
		if rows == 1 {
			return store.GitHubDeliveryClaimed, nil
		}
		status, err := s.q.GetGitHubDeliveryStatus(ctx, storedb.GetGitHubDeliveryStatusParams{
			ProjectID: projectID, DeliveryID: deliveryID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		switch status {
		case string(store.GitHubDeliveryComplete):
			return store.GitHubDeliveryComplete, nil
		case string(store.GitHubDeliveryProcessing):
			return store.GitHubDeliveryProcessing, nil
		case "failed":
			continue
		default:
			return "", errors.New("invalid GitHub delivery status")
		}
	}
	return "", errors.New("GitHub delivery claim changed concurrently")
}

func (s *Store) CompleteGitHubDelivery(ctx context.Context, projectID, deliveryID string) error {
	completedAt := now()
	_, err := s.q.CompleteGitHubDelivery(ctx, storedb.CompleteGitHubDeliveryParams{
		UpdatedAt: completedAt, CompletedAt: sqlText(completedAt), ProjectID: projectID, DeliveryID: deliveryID,
	})
	return err
}

func (s *Store) FailGitHubDelivery(ctx context.Context, projectID, deliveryID string) error {
	failedAt := now()
	_, err := s.q.FailGitHubDelivery(ctx, storedb.FailGitHubDeliveryParams{
		UpdatedAt: failedAt, FailedAt: sqlText(failedAt), ProjectID: projectID, DeliveryID: deliveryID,
	})
	return err
}

func (s *Store) GetGitHubPullRequestThread(ctx context.Context, projectID, repositoryID string, pullNumber int64) (string, error) {
	return s.q.GetGitHubPullRequestThread(ctx, storedb.GetGitHubPullRequestThreadParams{
		ProjectID: projectID, RepositoryID: repositoryID, PullNumber: pullNumber,
	})
}

func (s *Store) SetGitHubPullRequestThread(ctx context.Context, projectID, repositoryID string, pullNumber int64, rootMessageID string) (string, error) {
	_, err := s.q.InsertGitHubPullRequestThread(ctx, storedb.InsertGitHubPullRequestThreadParams{
		ProjectID: projectID, RepositoryID: repositoryID, PullNumber: pullNumber,
		RootMessageID: rootMessageID, UpdatedAt: now(),
	})
	if err != nil {
		return "", err
	}
	return s.GetGitHubPullRequestThread(ctx, projectID, repositoryID, pullNumber)
}

func hydrateProject(ctx context.Context, q *storedb.Queries, project *store.Project) error {
	repositories, err := q.ListProjectRepositories(ctx, project.ID)
	if err != nil {
		return err
	}
	project.Repositories = make([]store.ProjectRepository, 0, len(repositories))
	for _, repository := range repositories {
		project.Repositories = append(project.Repositories, store.ProjectRepository{
			ID: repository.ID, ProjectID: repository.ProjectID, Provider: repository.Provider,
			Owner: repository.Owner, Name: repository.Name, FullName: repository.FullName,
			URL: repository.Url, CreatedAt: repository.CreatedAt,
		})
	}
	members, err := q.ListProjectMembers(ctx, project.ID)
	if err != nil {
		return err
	}
	project.Members = make([]store.ProjectMember, 0, len(members))
	for _, member := range members {
		project.Members = append(project.Members, store.ProjectMember{
			User: storeUserFromDB(member.ID, member.Kind, member.OwnerUserID, member.DisplayName, member.Handle, member.AvatarUrl, member.CreatedAt),
			Role: member.Role,
		})
	}
	return nil
}

func projectFromGetRow(row storedb.GetProjectRow) store.Project {
	return projectFromFields(
		row.ID, row.WorkspaceID, row.Name, row.Slug, row.Description, row.IntegrationUserID,
		row.CreatedBy, row.CreatedAt, row.ChannelID, row.ChannelRouteID, row.ChannelName,
		row.ChannelKind, row.ChannelCreatedAt, row.ArchivedAt, row.ExternalManaged,
		row.ExternalRef, row.ExternalUrl, row.SidebarSection,
	)
}

func projectFromListRow(row storedb.ListProjectsRow) store.Project {
	return projectFromFields(
		row.ID, row.WorkspaceID, row.Name, row.Slug, row.Description, row.IntegrationUserID,
		row.CreatedBy, row.CreatedAt, row.ChannelID, row.ChannelRouteID, row.ChannelName,
		row.ChannelKind, row.ChannelCreatedAt, row.ArchivedAt, row.ExternalManaged,
		row.ExternalRef, row.ExternalUrl, row.SidebarSection,
	)
}

func projectFromFields(
	id, workspaceID, name, projectSlug, description, integrationUserID, createdBy, createdAt,
	channelID, channelRouteID, channelName, channelKind, channelCreatedAt string,
	archivedAt sql.NullString, externalManaged int64, externalRef, externalURL, sidebarSection sql.NullString,
) store.Project {
	return store.Project{
		ID: id, WorkspaceID: workspaceID, Name: name, Slug: projectSlug, Description: description,
		IntegrationUserID: integrationUserID, CreatedBy: createdBy, CreatedAt: createdAt,
		Channel: store.Channel{
			ID: channelID, RouteID: channelRouteID, WorkspaceID: workspaceID, Name: channelName,
			Kind: channelKind, CreatedAt: channelCreatedAt, ArchivedAt: ptrFromNull(archivedAt),
			ExternalManaged: externalManaged != 0, ExternalRef: ptrFromNull(externalRef),
			ExternalURL: ptrFromNull(externalURL), SidebarSection: ptrFromNull(sidebarSection),
		},
	}
}
