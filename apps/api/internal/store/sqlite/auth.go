package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
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

// CreateSessionForVerifiedPassword mints a session for a caller that has just
// verified a password, and only while the stored hash is still the one it
// verified. The argon2 comparison runs outside this call, so a password change
// can commit in between; that race ends here with
// store.ErrPasswordVerificationStale and no session, rather than a live session
// minted for a replaced secret.
func (s *Store) CreateSessionForVerifiedPassword(ctx context.Context, userID, verifiedHash string) (store.Session, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.TrimSpace(verifiedHash) == "" {
		return store.Session{}, errors.New("user id and verified password hash are required")
	}
	session := newSession(userID)
	rows, err := s.q.InsertSessionForVerifiedPassword(ctx, storedb.InsertSessionForVerifiedPasswordParams{
		ID:           session.ID,
		Token:        session.ID,
		TokenHash:    tokenHash(session.Token),
		CreatedAt:    session.CreatedAt,
		ExpiresAt:    session.ExpiresAt,
		UserID:       userID,
		VerifiedHash: verifiedHash,
	})
	if err != nil {
		return store.Session{}, err
	}
	if rows != 1 {
		return store.Session{}, store.ErrPasswordVerificationStale
	}
	return session, nil
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

// GetUserPasswordHash returns the stored hash for an account, or an empty
// string when the account has no password on file. Callers read the empty
// string as "not enrolled" rather than as a lookup failure.
func (s *Store) GetUserPasswordHash(ctx context.Context, userID string) (string, error) {
	hash, err := s.q.GetUserPasswordHash(ctx, strings.TrimSpace(userID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

// ChangeUserPassword replaces a password and ends the account's other sessions
// in one transaction, and reports how many sessions it ended. Both writes are
// conditional on the state the caller checked before it got here: the stored
// hash must still be input.VerifiedHash, and a caller that named a session must
// still hold a live one. Either condition failing rolls the whole change back,
// so a rotation that lost a race neither overwrites the winner's password nor
// signs the winner out.
func (s *Store) ChangeUserPassword(ctx context.Context, input store.ChangeUserPasswordInput) (int64, error) {
	userID := strings.TrimSpace(input.UserID)
	verifiedHash := strings.TrimSpace(input.VerifiedHash)
	newHash := strings.TrimSpace(input.NewHash)
	if userID == "" || verifiedHash == "" || newHash == "" {
		return 0, errors.New("user id, verified password hash, and new password hash are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	keepToken := strings.TrimSpace(input.KeepSessionToken)
	if keepToken != "" {
		expiresAt, err := qtx.GetLiveUserSessionExpiry(ctx, storedb.GetLiveUserSessionExpiryParams{
			UserID:    userID,
			TokenHash: tokenHash(keepToken),
		})
		if errors.Is(err, sql.ErrNoRows) {
			return 0, store.ErrSessionRevoked
		}
		if err != nil {
			return 0, err
		}
		if authTimestampExpired(expiresAt, time.Now()) {
			return 0, store.ErrSessionRevoked
		}
	}
	changed, err := qtx.ReplaceVerifiedUserPassword(ctx, storedb.ReplaceVerifiedUserPasswordParams{
		PasswordHash: newHash,
		UpdatedAt:    now(),
		UserID:       userID,
		VerifiedHash: verifiedHash,
	})
	if err != nil {
		return 0, err
	}
	if changed != 1 {
		return 0, store.ErrPasswordVerificationStale
	}
	revoked, err := qtx.RevokeUserSessionsExceptTokenHash(ctx, storedb.RevokeUserSessionsExceptTokenHashParams{
		RevokedAt:     sqlText(now()),
		UserID:        userID,
		KeepTokenHash: tokenHash(keepToken),
	})
	if err != nil {
		return 0, err
	}
	return revoked, tx.Commit()
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

func newSession(userID string) store.Session {
	return store.Session{
		ID:        newID("ses"),
		Token:     newID("sst"),
		UserID:    userID,
		CreatedAt: now(),
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339Nano),
	}
}

func createSessionTx(ctx context.Context, q *storedb.Queries, userID string) (store.Session, error) {
	session := newSession(userID)
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

func (s *Store) backfillAuthTokenHashes(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := backfillAuthMagicLinkTokenHashes(ctx, tx); err != nil {
		return err
	}
	if err := backfillSessionTokenHashes(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func backfillAuthMagicLinkTokenHashes(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, token FROM auth_magic_links WHERE token_hash = '' AND token <> '' AND token <> id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, token string
		if err := rows.Scan(&id, &token); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE auth_magic_links SET token_hash = ?, token = id WHERE id = ? AND token_hash = ''`, tokenHash(token), id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func backfillSessionTokenHashes(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, token FROM sessions WHERE token_hash = '' AND token <> '' AND token <> id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, token string
		if err := rows.Scan(&id, &token); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET token_hash = ?, token = id WHERE id = ? AND token_hash = ''`, tokenHash(token), id); err != nil {
			return err
		}
	}
	return rows.Err()
}
