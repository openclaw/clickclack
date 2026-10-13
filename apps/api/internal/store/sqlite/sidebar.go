package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) GetSidebarPreferences(ctx context.Context, userID string) (*store.SidebarPreferences, error) {
	rows, err := s.q.ListSidebarChannelOrder(ctx, userID)
	if err != nil {
		return nil, err
	}
	order := make(map[string][]string, len(rows))
	for _, row := range rows {
		ids, err := decodeSidebarChannelOrder(row.ChannelIds)
		if err != nil {
			// A row we cannot read is a stale cache, not a broken account. The
			// workspace falls back to the server's default ordering.
			continue
		}
		// A row holding an empty list is a cleared order, which is not the same
		// as never having saved one: the client needs the key back so it can
		// drop its own cached order instead of restoring it.
		if ids == nil {
			ids = []string{}
		}
		order[row.WorkspaceID] = ids
	}
	if len(order) == 0 {
		return nil, nil
	}
	return &store.SidebarPreferences{ChannelOrder: order}, nil
}

func updateSidebarPreferences(ctx context.Context, q *storedb.Queries, userID string, patch store.SidebarPreferencesPatch, timestamp string) error {
	for workspaceID, order := range patch.ChannelOrder {
		if _, err := q.RequireMembership(ctx, storedb.RequireMembershipParams{WorkspaceID: workspaceID, UserID: userID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotWorkspaceMember
			}
			return err
		}
		channelIDs, err := q.ListWorkspaceChannelIDs(ctx, workspaceID)
		if err != nil {
			return err
		}
		// A cleared order is stored as a row holding an empty list rather than
		// deleted, so a later read can tell "cleared" from "never saved". The
		// row goes away only when the membership it hangs off does.
		filtered := store.FilterSidebarChannelOrder(order, channelIDs)
		encoded, err := json.Marshal(filtered)
		if err != nil {
			return err
		}
		if err := q.UpsertSidebarChannelOrder(ctx, storedb.UpsertSidebarChannelOrderParams{
			UserID:      userID,
			WorkspaceID: workspaceID,
			ChannelIds:  string(encoded),
			UpdatedAt:   timestamp,
		}); err != nil {
			return err
		}
	}
	return nil
}

func decodeSidebarChannelOrder(raw string) ([]string, error) {
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}
