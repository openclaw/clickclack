package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) AddWorkspaceMember(ctx context.Context, workspaceID, userID, role string) error {
	if role == "" {
		role = "member"
	}
	return s.q.InsertWorkspaceMember(ctx, storedb.InsertWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
		CreatedAt:   now(),
	})
}

func (s *Store) EnsureDefaultWorkspaceMember(ctx context.Context, userID string) (store.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)

	row, err := qtx.FirstWorkspace(ctx)
	workspace := storeWorkspaceFromFirstWorkspace(row)
	if err != nil && err != sql.ErrNoRows {
		return store.Workspace{}, err
	}
	if err == sql.ErrNoRows {
		workspace = store.Workspace{ID: newID("wsp"), Name: "ClickClack", Slug: "clickclack", CreatedAt: now()}
		insertedWorkspace := false
		for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
			workspaceRouteID, err := newRouteID('T')
			if err != nil {
				return store.Workspace{}, err
			}
			workspace.RouteID = workspaceRouteID
			if err := qtx.InsertWorkspace(ctx, storedb.InsertWorkspaceParams{
				ID:        workspace.ID,
				RouteID:   sqlText(workspace.RouteID),
				Name:      workspace.Name,
				Slug:      workspace.Slug,
				CreatedAt: workspace.CreatedAt,
			}); err != nil {
				if isRouteIDConflict(err) {
					continue
				}
				return store.Workspace{}, err
			}
			insertedWorkspace = true
			break
		}
		if !insertedWorkspace {
			return store.Workspace{}, errors.New("could not create workspace route_id after collision retries")
		}
		channelID := newID("chn")
		insertedChannel := false
		for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
			channelRouteID, err := newRouteID('C')
			if err != nil {
				return store.Workspace{}, err
			}
			if err := qtx.InsertDefaultChannel(ctx, storedb.InsertDefaultChannelParams{
				ID:          channelID,
				RouteID:     sqlText(channelRouteID),
				WorkspaceID: workspace.ID,
				CreatedAt:   workspace.CreatedAt,
			}); err != nil {
				if isRouteIDConflict(err) {
					continue
				}
				return store.Workspace{}, err
			}
			insertedChannel = true
			break
		}
		if !insertedChannel {
			return store.Workspace{}, errors.New("could not create channel route_id after collision retries")
		}
	}
	if err := qtx.InsertDefaultWorkspaceMember(ctx, storedb.InsertDefaultWorkspaceMemberParams{
		WorkspaceID: workspace.ID,
		UserID:      userID,
		CreatedAt:   now(),
	}); err != nil {
		return store.Workspace{}, err
	}
	return workspace, tx.Commit()
}
