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
	user, err := getOrCreateMagicUser(ctx, qtx, link.Email, link.DisplayName)
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	usedAt := now()
	if err := qtx.MarkMagicLinkUsed(ctx, storedb.MarkMagicLinkUsedParams{UsedAt: sqlText(usedAt), ID: link.ID}); err != nil {
		return store.User{}, store.Session{}, err
	}
	session, err := createSessionTx(ctx, qtx, user.ID)
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	return user, session, tx.Commit()
}

func (s *Store) GetSessionUser(ctx context.Context, token string) (store.User, error) {
	token = strings.TrimSpace(token)
	row, err := s.q.GetSessionUser(ctx, storedb.GetSessionUserParams{TokenHash: tokenHash(token), Now: now()})
	if err != nil {
		return store.User{}, err
	}
	return storeUserFromGetSessionUser(row), nil
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

func getOrCreateMagicUser(ctx context.Context, q *storedb.Queries, email, displayName string) (store.User, error) {
	row, err := q.GetUserByIdentityEmail(ctx, email)
	if err == nil {
		return storeUserFromIdentityEmail(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.User{}, err
	}
	user := store.User{ID: newID("usr"), Kind: "human", DisplayName: strings.TrimSpace(displayName), Handle: "", AvatarURL: "", CreatedAt: now()}
	if user.DisplayName == "" {
		user.DisplayName = email
	}
	if err := q.InsertHumanUser(ctx, storedb.InsertHumanUserParams{ID: user.ID, DisplayName: user.DisplayName, AvatarUrl: "", CreatedAt: user.CreatedAt}); err != nil {
		return store.User{}, err
	}
	err = q.InsertIdentity(ctx, storedb.InsertIdentityParams{
		ID:              newID("idn"),
		UserID:          user.ID,
		Provider:        "magic",
		ProviderSubject: email,
		Email:           email,
		CreatedAt:       user.CreatedAt,
	})
	return user, err
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
