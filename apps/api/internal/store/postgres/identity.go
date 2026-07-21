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
	lookup := storedb.GetUserByIdentityProviderSubjectParams{Provider: provider, ProviderSubject: subject}
	row, err := s.q.GetUserByIdentityProviderSubject(ctx, lookup)
	if err == nil {
		user := storeUserFromIdentityProviderSubject(row)
		email := strings.TrimSpace(input.Email)
		if email != "" {
			if err := s.q.UpdateIdentityEmailIfEmpty(ctx, storedb.UpdateIdentityEmailIfEmptyParams{
				Email:           email,
				Provider:        provider,
				ProviderSubject: subject,
			}); err != nil {
				return store.User{}, err
			}
		}
		storedEmail, emailErr := s.q.GetIdentityEmailForUser(ctx, user.ID)
		if emailErr == nil {
			email = storedEmail
		} else if !errors.Is(emailErr, sql.ErrNoRows) {
			return store.User{}, emailErr
		}
		explicitAvatarURL := strings.TrimSpace(input.AvatarURL)
		fallbackURL := store.ResolveAvatarURL("", email)
		if explicitAvatarURL != "" {
			updated, err := s.q.SetProviderAvatarUnlessExplicit(ctx, storedb.SetProviderAvatarUnlessExplicitParams{
				ID:          user.ID,
				AvatarUrl:   explicitAvatarURL,
				FallbackUrl: fallbackURL,
			})
			if err != nil {
				return store.User{}, err
			}
			if updated == 0 {
				latestEmail, emailErr := s.q.GetIdentityEmailForUser(ctx, user.ID)
				if emailErr == nil {
					_, err = s.q.SetProviderAvatarUnlessExplicit(ctx, storedb.SetProviderAvatarUnlessExplicitParams{
						ID:          user.ID,
						AvatarUrl:   explicitAvatarURL,
						FallbackUrl: store.ResolveAvatarURL("", latestEmail),
					})
					if err != nil {
						return store.User{}, err
					}
				} else if !errors.Is(emailErr, sql.ErrNoRows) {
					return store.User{}, emailErr
				}
			}
		} else if fallbackURL != "" {
			if err := s.q.SetUserAvatarIfEmpty(ctx, storedb.SetUserAvatarIfEmptyParams{ID: user.ID, AvatarUrl: fallbackURL}); err != nil {
				return store.User{}, err
			}
		}
		row, err = s.q.GetUserByIdentityProviderSubject(ctx, lookup)
		if err != nil {
			return store.User{}, err
		}
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
		AvatarURL:   store.ResolveAvatarURL(input.AvatarURL, input.Email),
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
