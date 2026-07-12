package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) ResolveRouteTarget(ctx context.Context, userID, workspaceRouteID, targetRouteID string) (store.RouteTarget, error) {
	workspaceRouteID = strings.TrimSpace(workspaceRouteID)
	targetRouteID = strings.TrimSpace(targetRouteID)
	if workspaceRouteID == "" || targetRouteID == "" {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	workspaceRow, err := s.q.GetWorkspaceByRouteID(ctx, sqlText(workspaceRouteID))
	if err != nil {
		return store.RouteTarget{}, err
	}
	workspace := store.Workspace{
		ID:        workspaceRow.ID,
		RouteID:   workspaceRow.RouteID,
		Name:      workspaceRow.Name,
		Slug:      workspaceRow.Slug,
		IconURL:   workspaceRow.IconUrl,
		CreatedAt: workspaceRow.CreatedAt,
	}
	if err := s.requireMembership(ctx, workspace.ID, userID); err != nil {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	return s.resolveTargetInWorkspace(ctx, userID, workspace, targetRouteID, false)
}

func (s *Store) ResolveLegacyRouteTarget(ctx context.Context, userID, workspaceID, targetID string) (store.RouteTarget, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	targetID = strings.TrimSpace(targetID)
	if workspaceID == "" || targetID == "" {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	workspace, err := s.GetWorkspace(ctx, workspaceID, userID)
	if err != nil {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	return s.resolveTargetInWorkspace(ctx, userID, workspace, targetID, true)
}

func (s *Store) resolveTargetInWorkspace(ctx context.Context, userID string, workspace store.Workspace, targetID string, legacy bool) (store.RouteTarget, error) {
	switch {
	case (!legacy && strings.HasPrefix(targetID, "C")) || (legacy && strings.HasPrefix(targetID, "chn_")):
		return s.resolveChannelRouteTarget(ctx, userID, workspace, targetID, legacy)
	case (!legacy && strings.HasPrefix(targetID, "D")) || (legacy && strings.HasPrefix(targetID, "dm_")):
		return s.resolveDirectRouteTarget(ctx, userID, workspace, targetID, legacy)
	case (!legacy && strings.HasPrefix(targetID, "M")) || (legacy && strings.HasPrefix(targetID, "msg_")):
		return s.resolveThreadRouteTarget(ctx, userID, workspace, targetID, legacy)
	default:
		return store.RouteTarget{}, sql.ErrNoRows
	}
}

func (s *Store) resolveChannelRouteTarget(ctx context.Context, userID string, workspace store.Workspace, targetID string, legacy bool) (store.RouteTarget, error) {
	var channel store.Channel
	var err error
	if legacy {
		var row storedb.GetChannelByIDAndWorkspaceRow
		row, err = s.q.GetChannelByIDAndWorkspace(ctx, storedb.GetChannelByIDAndWorkspaceParams{WorkspaceID: workspace.ID, ID: targetID})
		channel = store.Channel{ID: row.ID, RouteID: row.RouteID, WorkspaceID: row.WorkspaceID, Name: row.Name, Kind: row.Kind, CreatedAt: row.CreatedAt, ArchivedAt: ptrFromNull(row.ArchivedAt)}
	} else {
		var row storedb.GetChannelByRouteIDAndWorkspaceRow
		row, err = s.q.GetChannelByRouteIDAndWorkspace(ctx, storedb.GetChannelByRouteIDAndWorkspaceParams{WorkspaceID: workspace.ID, RouteID: sqlText(targetID)})
		channel = store.Channel{ID: row.ID, RouteID: row.RouteID, WorkspaceID: row.WorkspaceID, Name: row.Name, Kind: row.Kind, CreatedAt: row.CreatedAt, ArchivedAt: ptrFromNull(row.ArchivedAt)}
	}
	if err != nil || channel.RouteID == "" {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	if err := s.requireGuestChannelAccess(ctx, workspace.ID, channel.ID, userID); err != nil {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	return store.RouteTarget{
		WorkspaceID:      workspace.ID,
		WorkspaceRouteID: workspace.RouteID,
		TargetType:       "channel",
		TargetID:         channel.ID,
		TargetRouteID:    channel.RouteID,
		CanonicalPath:    routeCanonicalPath(workspace.RouteID, channel.RouteID),
	}, nil
}

func (s *Store) resolveDirectRouteTarget(ctx context.Context, userID string, workspace store.Workspace, targetID string, legacy bool) (store.RouteTarget, error) {
	var dm store.DirectConversation
	var err error
	if legacy {
		var row storedb.GetDirectByIDAndWorkspaceRow
		row, err = s.q.GetDirectByIDAndWorkspace(ctx, storedb.GetDirectByIDAndWorkspaceParams{WorkspaceID: workspace.ID, ID: targetID, UserID: userID})
		dm = store.DirectConversation{ID: row.ID, RouteID: row.RouteID, WorkspaceID: row.WorkspaceID, CreatedAt: row.CreatedAt}
	} else {
		var row storedb.GetDirectByRouteIDAndWorkspaceRow
		row, err = s.q.GetDirectByRouteIDAndWorkspace(ctx, storedb.GetDirectByRouteIDAndWorkspaceParams{WorkspaceID: workspace.ID, RouteID: sqlText(targetID), UserID: userID})
		dm = store.DirectConversation{ID: row.ID, RouteID: row.RouteID, WorkspaceID: row.WorkspaceID, CreatedAt: row.CreatedAt}
	}
	if err != nil || dm.RouteID == "" {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	if err := s.requireDirectAccess(ctx, dm.ID, userID); err != nil {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	return store.RouteTarget{
		WorkspaceID:      workspace.ID,
		WorkspaceRouteID: workspace.RouteID,
		TargetType:       "direct",
		TargetID:         dm.ID,
		TargetRouteID:    dm.RouteID,
		CanonicalPath:    routeCanonicalPath(workspace.RouteID, dm.RouteID),
	}, nil
}

func (s *Store) resolveThreadRouteTarget(ctx context.Context, userID string, workspace store.Workspace, targetID string, legacy bool) (store.RouteTarget, error) {
	var root store.Message
	var err error
	if legacy {
		root, err = getMessage(ctx, s.db, targetID)
		if err == nil && root.WorkspaceID != workspace.ID {
			return store.RouteTarget{}, sql.ErrNoRows
		}
		if err == nil {
			err = s.requireMessageAccess(ctx, root, userID)
		}
		if err == nil {
			root, err = s.EnsureThreadRouteID(ctx, userID, root.ID)
		}
	} else {
		root, err = scanMessage(s.db.QueryRowContext(ctx, messageSelect()+`
			WHERE m.workspace_id = ? AND m.route_id = ? AND m.parent_message_id IS NULL`, workspace.ID, targetID))
		if err == nil {
			err = s.requireMessageAccess(ctx, root, userID)
		}
	}
	if err != nil || root.WorkspaceID != workspace.ID || root.RouteID == "" || root.ParentMessageID != nil {
		return store.RouteTarget{}, sql.ErrNoRows
	}
	target := store.RouteTarget{
		WorkspaceID:      workspace.ID,
		WorkspaceRouteID: workspace.RouteID,
		TargetType:       "thread",
		TargetID:         root.ID,
		TargetRouteID:    root.RouteID,
		CanonicalPath:    routeCanonicalPath(workspace.RouteID, root.RouteID),
	}
	if root.ChannelID != "" {
		parentRouteID, err := s.channelRouteID(ctx, workspace.ID, root.ChannelID)
		if err != nil || parentRouteID == "" {
			return store.RouteTarget{}, sql.ErrNoRows
		}
		target.ParentType = "channel"
		target.ParentID = root.ChannelID
		target.ParentRouteID = parentRouteID
		return target, nil
	}
	if root.DirectConversationID != "" {
		parentRouteID, err := s.directRouteID(ctx, userID, workspace.ID, root.DirectConversationID)
		if err != nil || parentRouteID == "" {
			return store.RouteTarget{}, sql.ErrNoRows
		}
		target.ParentType = "direct"
		target.ParentID = root.DirectConversationID
		target.ParentRouteID = parentRouteID
		return target, nil
	}
	return store.RouteTarget{}, sql.ErrNoRows
}

func (s *Store) channelRouteID(ctx context.Context, workspaceID, channelID string) (string, error) {
	return s.q.ChannelRouteID(ctx, storedb.ChannelRouteIDParams{WorkspaceID: workspaceID, ID: channelID})
}

func (s *Store) directRouteID(ctx context.Context, userID, workspaceID, conversationID string) (string, error) {
	return s.q.DirectRouteID(ctx, storedb.DirectRouteIDParams{WorkspaceID: workspaceID, ID: conversationID, UserID: userID})
}

func routeCanonicalPath(workspaceRouteID, targetRouteID string) string {
	return "/app/" + url.PathEscape(workspaceRouteID) + "/" + url.PathEscape(targetRouteID)
}
