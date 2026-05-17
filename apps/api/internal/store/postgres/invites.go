package postgres

import (
	"context"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) CreateInvite(ctx context.Context, workspaceID, createdBy string) (store.Invite, error) {
	if err := s.requireMembership(ctx, workspaceID, createdBy); err != nil {
		return store.Invite{}, err
	}
	invite := store.Invite{
		ID:          newID("inv"),
		WorkspaceID: workspaceID,
		Token:       newID("tok"),
		CreatedBy:   createdBy,
		CreatedAt:   now(),
	}
	return invite, s.q.InsertInvite(ctx, storedb.InsertInviteParams{
		ID:          invite.ID,
		WorkspaceID: invite.WorkspaceID,
		Token:       invite.Token,
		CreatedBy:   invite.CreatedBy,
		CreatedAt:   invite.CreatedAt,
	})
}
