package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) SyncIdentities(ctx context.Context, input store.IdentitySyncInput) (store.IdentitySyncReport, error) {
	input, err := store.NormalizeIdentitySync(input)
	if err != nil {
		return store.IdentitySyncReport{}, err
	}
	emails := []string{}
	for _, profile := range input.Profiles {
		if profile.MergedInto == nil {
			emails = append(emails, profile.Emails...)
		}
	}
	encodedEmails, err := json.Marshal(emails)
	if err != nil {
		return store.IdentitySyncReport{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return store.IdentitySyncReport{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	rows, err := qtx.ListIdentitySyncUsers(ctx, storedb.ListIdentitySyncUsersParams{Source: input.Source, Emails: string(encodedEmails)})
	if err != nil {
		return store.IdentitySyncReport{}, err
	}
	identities := make([]store.IdentitySyncRow, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, store.IdentitySyncRow{
			User:     store.User{ID: row.ID, Kind: row.Kind, DisplayName: row.DisplayName, Handle: row.Handle, AvatarURL: row.AvatarUrl},
			Provider: row.Provider, Subject: row.ProviderSubject, Email: row.Email,
		})
	}
	changes, report, err := store.PlanIdentitySync(input, identities)
	if err != nil {
		return store.IdentitySyncReport{}, err
	}
	for _, change := range changes {
		if change.Link {
			if err := qtx.InsertIdentity(ctx, storedb.InsertIdentityParams{
				ID: newID("idn"), UserID: change.ID, Provider: input.Source,
				ProviderSubject: change.ProfileID, Email: "", CreatedAt: now(),
			}); err != nil {
				return store.IdentitySyncReport{}, err
			}
		}
		if change.Update {
			if err := qtx.UpdateUserProfile(ctx, storedb.UpdateUserProfileParams{
				ID: change.ID, DisplayName: change.DisplayName, Handle: change.Handle, AvatarUrl: change.AvatarURL,
			}); err != nil {
				return store.IdentitySyncReport{}, err
			}
			if err := qtx.UpdateWorkspaceMemberSortKeys(ctx, storedb.UpdateWorkspaceMemberSortKeysParams{
				UserID: change.ID, DisplayName: change.DisplayName, Handle: change.Handle,
			}); err != nil {
				return store.IdentitySyncReport{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return store.IdentitySyncReport{}, err
	}
	return report, nil
}
