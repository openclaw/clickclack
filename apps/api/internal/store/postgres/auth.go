package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) CreateMagicLink(ctx context.Context, email, displayName string) (store.MagicLink, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return store.MagicLink{}, errors.New("email is required")
	}
	link := store.MagicLink{
		ID:          newID("mln"),
		Token:       newID("mgt"),
		Email:       email,
		DisplayName: strings.TrimSpace(displayName),
		CreatedAt:   now(),
		ExpiresAt:   time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339Nano),
	}
	return link, s.q.InsertMagicLink(ctx, storedb.InsertMagicLinkParams{
		ID:          link.ID,
		Token:       link.ID,
		TokenHash:   tokenHash(link.Token),
		Email:       link.Email,
		DisplayName: link.DisplayName,
		CreatedAt:   link.CreatedAt,
		ExpiresAt:   link.ExpiresAt,
	})
}

func (s *Store) ConsumeMagicLink(ctx context.Context, token string) (store.User, store.Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	token = strings.TrimSpace(token)
	linkRow, err := qtx.GetMagicLinkByToken(ctx, tokenHash(token))
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	link := storeMagicLinkFromDB(linkRow)
	if link.UsedAt != nil {
		return store.User{}, store.Session{}, errors.New("magic link already used")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, link.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		return store.User{}, store.Session{}, errors.New("magic link expired")
	}
	user, err := getOrCreateUserByEmail(ctx, qtx, "magic", link.Email, link.DisplayName)
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	usedAt := now()
	rows, err := qtx.MarkMagicLinkUsed(ctx, storedb.MarkMagicLinkUsedParams{UsedAt: sqlText(usedAt), ID: link.ID, Now: usedAt})
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	if rows != 1 {
		return store.User{}, store.Session{}, errors.New("magic link already used")
	}
	session, err := createSessionTx(ctx, qtx, user.ID)
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	return user, session, tx.Commit()
}

func (s *Store) GetSessionUser(ctx context.Context, token string) (store.User, error) {
	token = strings.TrimSpace(token)
	row, err := s.q.GetSessionUser(ctx, tokenHash(token))
	if err != nil {
		return store.User{}, err
	}
	if authTimestampExpired(row.SessionExpiresAt, time.Now()) {
		return store.User{}, errors.New("session expired")
	}
	return storeUserFromGetSessionUser(row), nil
}

func authTimestampExpired(value string, current time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	return err != nil || !current.UTC().Before(expiresAt)
}

func (s *Store) CreateSession(ctx context.Context, userID string) (store.Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Session{}, err
	}
	defer tx.Rollback()
	session, err := createSessionTx(ctx, s.q.WithTx(tx), userID)
	if err != nil {
		return store.Session{}, err
	}
	return session, tx.Commit()
}

// GetPasswordLogin resolves a password login identifier, which may be an
// identity email or a handle, to its account and stored hash.
func (s *Store) GetPasswordLogin(ctx context.Context, identifier string) (store.PasswordLogin, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return store.PasswordLogin{}, sql.ErrNoRows
	}
	rows, err := s.q.ListPasswordLoginsByIdentifier(ctx, identifier)
	if err != nil {
		return store.PasswordLogin{}, err
	}
	if len(rows) == 0 {
		return store.PasswordLogin{}, sql.ErrNoRows
	}
	if len(rows) > 1 {
		return store.PasswordLogin{}, store.ErrAmbiguousUserIdentifier
	}
	row := rows[0]
	return store.PasswordLogin{
		User:         storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt),
		PasswordHash: row.PasswordHash,
	}, nil
}

// SetUserPassword enables password login for an account, or replaces the hash
// already on file.
func (s *Store) SetUserPassword(ctx context.Context, userID, passwordHash string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.TrimSpace(passwordHash) == "" {
		return errors.New("user id and password hash are required")
	}
	return s.q.UpsertUserPassword(ctx, storedb.UpsertUserPasswordParams{
		UserID:       userID,
		PasswordHash: passwordHash,
		UpdatedAt:    now(),
	})
}

// ClearUserPassword disables password login for an account.
func (s *Store) ClearUserPassword(ctx context.Context, userID string) error {
	rows, err := s.q.DeleteUserPassword(ctx, strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RevokeSession ends a session by token. Revoking an unknown or already
// revoked session is not an error so that sign-out stays idempotent.
func (s *Store) RevokeSession(ctx context.Context, token string) error {
	_, err := s.q.RevokeSessionByTokenHash(ctx, storedb.RevokeSessionByTokenHashParams{
		RevokedAt: sqlText(now()),
		TokenHash: tokenHash(strings.TrimSpace(token)),
	})
	return err
}

func (s *Store) GetOrCreateUserByEmail(ctx context.Context, provider, email, displayName string) (store.User, error) {
	provider = strings.TrimSpace(provider)
	email = strings.ToLower(strings.TrimSpace(email))
	if provider == "" || email == "" {
		return store.User{}, errors.New("identity provider and email are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, err
	}
	defer tx.Rollback()
	user, err := getOrCreateUserByEmail(ctx, s.q.WithTx(tx), provider, email, displayName)
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		return user, nil
	}
	_ = tx.Rollback()
	row, lookupErr := s.q.GetUserByIdentityEmail(ctx, email)
	if lookupErr == nil {
		return ensureUserAvatarForEmail(ctx, s.q, storeUserFromIdentityEmail(row), email)
	}
	return store.User{}, err
}

func getOrCreateUserByEmail(ctx context.Context, q *storedb.Queries, provider, email, displayName string) (store.User, error) {
	provider = strings.TrimSpace(provider)
	email = strings.ToLower(strings.TrimSpace(email))
	if provider == "" || email == "" {
		return store.User{}, errors.New("identity provider and email are required")
	}
	row, err := q.GetUserByIdentityEmail(ctx, email)
	if err == nil {
		return ensureUserAvatarForEmail(ctx, q, storeUserFromIdentityEmail(row), email)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.User{}, err
	}
	user := store.User{ID: newID("usr"), Kind: "human", DisplayName: strings.TrimSpace(displayName), Handle: "", AvatarURL: store.ResolveAvatarURL("", email), CreatedAt: now()}
	if user.DisplayName == "" {
		user.DisplayName = email
	}
	if err := q.InsertHumanUser(ctx, storedb.InsertHumanUserParams{ID: user.ID, DisplayName: user.DisplayName, AvatarUrl: user.AvatarURL, CreatedAt: user.CreatedAt}); err != nil {
		return store.User{}, err
	}
	err = q.InsertIdentity(ctx, storedb.InsertIdentityParams{
		ID:              newID("idn"),
		UserID:          user.ID,
		Provider:        provider,
		ProviderSubject: email,
		Email:           email,
		CreatedAt:       user.CreatedAt,
	})
	return user, err
}

func ensureUserAvatarForEmail(ctx context.Context, q *storedb.Queries, user store.User, email string) (store.User, error) {
	if user.AvatarURL != "" {
		return user, nil
	}
	avatarURL := store.ResolveAvatarURL("", email)
	if avatarURL == "" {
		return user, nil
	}
	if err := q.SetUserAvatarIfEmpty(ctx, storedb.SetUserAvatarIfEmptyParams{ID: user.ID, AvatarUrl: avatarURL}); err != nil {
		return store.User{}, err
	}
	row, err := q.GetUserByIdentityEmail(ctx, email)
	if err != nil {
		return store.User{}, err
	}
	return storeUserFromIdentityEmail(row), nil
}

func createSessionTx(ctx context.Context, q *storedb.Queries, userID string) (store.Session, error) {
	session := store.Session{
		ID:        newID("ses"),
		Token:     newID("sst"),
		UserID:    userID,
		CreatedAt: now(),
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339Nano),
	}
	return session, q.InsertSession(ctx, storedb.InsertSessionParams{
		ID:        session.ID,
		Token:     session.ID,
		TokenHash: tokenHash(session.Token),
		UserID:    session.UserID,
		CreatedAt: session.CreatedAt,
		ExpiresAt: session.ExpiresAt,
	})
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
