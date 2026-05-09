package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Store) UpsertIdentityUser(ctx context.Context, input store.UpsertIdentityUserInput) (store.User, error) {
	provider := strings.TrimSpace(input.Provider)
	subject := strings.TrimSpace(input.ProviderSubject)
	if provider == "" || subject == "" {
		return store.User{}, errors.New("identity provider and subject are required")
	}
	user, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.display_name, u.handle, u.avatar_url, u.created_at
		FROM identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.provider = ? AND i.provider_subject = ?`, provider, subject))
	if err == nil {
		return s.hydrateUserNotificationSettings(ctx, user)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.User{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, err
	}
	defer tx.Rollback()
	user = store.User{
		ID:          newID("usr"),
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES (?, ?, ?, ?)`, user.ID, user.DisplayName, user.AvatarURL, user.CreatedAt); err != nil {
		return store.User{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO identities (id, user_id, provider, provider_subject, email, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, newID("idn"), user.ID, provider, subject, strings.TrimSpace(input.Email), user.CreatedAt)
	if err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.User{}, err
	}
	return s.hydrateUserNotificationSettings(ctx, user)
}
