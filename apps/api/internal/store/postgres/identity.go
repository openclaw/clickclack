package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) UpsertIdentityUser(ctx context.Context, input store.UpsertIdentityUserInput) (store.User, error) {
	provider := strings.TrimSpace(input.Provider)
	subject := strings.TrimSpace(input.ProviderSubject)
	if provider == "" || subject == "" {
		return store.User{}, errors.New("identity provider and subject are required")
	}
	row, err := s.q.GetUserByIdentityProviderSubject(ctx, storedb.GetUserByIdentityProviderSubjectParams{Provider: provider, ProviderSubject: subject})
	if err == nil {
		return s.hydrateUserNotificationSettings(ctx, storeUserFromIdentityProviderSubject(row))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.User{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	user := store.User{
		ID:          newID("usr"),
		Kind:        "human",
		DisplayName: strings.TrimSpace(input.DisplayName),
		Handle:      "",
		AvatarURL:   strings.TrimSpace(input.AvatarURL),
		CreatedAt:   now(),
	}
	if user.DisplayName == "" {
		user.DisplayName = strings.TrimSpace(input.Email)
	}
	if user.DisplayName == "" {
		user.DisplayName = provider + ":" + subject
	}
	if err := qtx.InsertHumanUser(ctx, storedb.InsertHumanUserParams{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		AvatarUrl:   user.AvatarURL,
		CreatedAt:   user.CreatedAt,
	}); err != nil {
		return store.User{}, err
	}
	if err := qtx.InsertIdentity(ctx, storedb.InsertIdentityParams{
		ID:              newID("idn"),
		UserID:          user.ID,
		Provider:        provider,
		ProviderSubject: subject,
		Email:           strings.TrimSpace(input.Email),
		CreatedAt:       user.CreatedAt,
	}); err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.User{}, err
	}
	return s.hydrateUserNotificationSettings(ctx, user)
}
