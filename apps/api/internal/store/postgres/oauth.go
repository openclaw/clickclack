package postgres

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

const (
	maxPendingOAuthTransactions        = 8192
	maxPendingOAuthTransactionsPerUser = 8
	maxPendingDesktopOAuthGrants       = 4096
)

func (s *Store) CreateOAuthTransaction(ctx context.Context, transaction store.OAuthTransaction) error {
	if err := validateOAuthTransaction(transaction); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.DeleteExpiredOAuthTransactions(ctx, transaction.CreatedAt.Unix()); err != nil {
		return err
	}
	count, err := qtx.CountOAuthTransactions(ctx)
	if err != nil {
		return err
	}
	if count >= maxPendingOAuthTransactions {
		return store.ErrOAuthCapacityExceeded
	}
	count, err = qtx.CountOAuthTransactionsForBinding(ctx, transaction.BrowserBindingHash)
	if err != nil {
		return err
	}
	if count >= maxPendingOAuthTransactionsPerUser {
		return store.ErrOAuthCapacityExceeded
	}
	if transaction.ID == "" {
		transaction.ID = newID("oat")
	}
	if err := qtx.InsertOAuthTransaction(ctx, storedb.InsertOAuthTransactionParams{
		ID:                 transaction.ID,
		StateHash:          transaction.StateHash,
		BrowserBindingHash: transaction.BrowserBindingHash,
		Mode:               transaction.Mode,
		PkceVerifier:       transaction.PKCEVerifier,
		DesktopChallenge:   transaction.DesktopChallenge,
		DesktopProtocol:    transaction.DesktopProtocol,
		CreatedAtUnix:      transaction.CreatedAt.Unix(),
		ExpiresAtUnix:      transaction.ExpiresAt.Unix(),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ConsumeOAuthTransaction(ctx context.Context, stateHash, browserBindingHash string, current time.Time) (store.OAuthTransaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.OAuthTransaction{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	row, err := qtx.GetOAuthTransactionForConsume(ctx, strings.TrimSpace(stateHash))
	if errors.Is(err, sql.ErrNoRows) {
		return store.OAuthTransaction{}, store.ErrOAuthTransactionInvalid
	}
	if err != nil {
		return store.OAuthTransaction{}, err
	}
	if row.ExpiresAtUnix <= current.Unix() {
		if _, err := qtx.DeleteOAuthTransaction(ctx, storedb.DeleteOAuthTransactionParams{ID: row.ID, StateHash: row.StateHash}); err != nil {
			return store.OAuthTransaction{}, err
		}
		if err := tx.Commit(); err != nil {
			return store.OAuthTransaction{}, err
		}
		return store.OAuthTransaction{}, store.ErrOAuthTransactionInvalid
	}
	if subtle.ConstantTimeCompare([]byte(row.BrowserBindingHash), []byte(strings.TrimSpace(browserBindingHash))) != 1 {
		return store.OAuthTransaction{}, store.ErrOAuthTransactionInvalid
	}
	deleted, err := qtx.DeleteOAuthTransaction(ctx, storedb.DeleteOAuthTransactionParams{ID: row.ID, StateHash: row.StateHash})
	if err != nil {
		return store.OAuthTransaction{}, err
	}
	if deleted != 1 {
		return store.OAuthTransaction{}, store.ErrOAuthTransactionInvalid
	}
	if err := tx.Commit(); err != nil {
		return store.OAuthTransaction{}, err
	}
	return oauthTransactionFromDB(row), nil
}

func (s *Store) CreateDesktopOAuthGrant(ctx context.Context, grant store.DesktopOAuthGrant) error {
	if err := validateDesktopOAuthGrant(grant); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.DeleteExpiredDesktopOAuthGrants(ctx, grant.CreatedAt.Unix()); err != nil {
		return err
	}
	count, err := qtx.CountDesktopOAuthGrants(ctx)
	if err != nil {
		return err
	}
	if count >= maxPendingDesktopOAuthGrants {
		return store.ErrOAuthCapacityExceeded
	}
	if grant.ID == "" {
		grant.ID = newID("odg")
	}
	if err := qtx.InsertDesktopOAuthGrant(ctx, storedb.InsertDesktopOAuthGrantParams{
		ID:               grant.ID,
		GrantHash:        grant.GrantHash,
		UserID:           grant.UserID,
		DesktopChallenge: grant.DesktopChallenge,
		CreatedAtUnix:    grant.CreatedAt.Unix(),
		ExpiresAtUnix:    grant.ExpiresAt.Unix(),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ConsumeDesktopOAuthGrant(ctx context.Context, grantHash, desktopChallenge string, current time.Time) (store.Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Session{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	row, err := qtx.GetDesktopOAuthGrantForConsume(ctx, strings.TrimSpace(grantHash))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, store.ErrDesktopOAuthGrantInvalid
	}
	if err != nil {
		return store.Session{}, err
	}
	if row.ExpiresAtUnix <= current.Unix() {
		if _, err := qtx.DeleteDesktopOAuthGrant(ctx, storedb.DeleteDesktopOAuthGrantParams{ID: row.ID, GrantHash: row.GrantHash}); err != nil {
			return store.Session{}, err
		}
		if err := tx.Commit(); err != nil {
			return store.Session{}, err
		}
		return store.Session{}, store.ErrDesktopOAuthGrantInvalid
	}
	if subtle.ConstantTimeCompare([]byte(row.DesktopChallenge), []byte(strings.TrimSpace(desktopChallenge))) != 1 {
		return store.Session{}, store.ErrDesktopOAuthGrantInvalid
	}
	deleted, err := qtx.DeleteDesktopOAuthGrant(ctx, storedb.DeleteDesktopOAuthGrantParams{ID: row.ID, GrantHash: row.GrantHash})
	if err != nil {
		return store.Session{}, err
	}
	if deleted != 1 {
		return store.Session{}, store.ErrDesktopOAuthGrantInvalid
	}
	session, err := createSessionTx(ctx, qtx, row.UserID)
	if err != nil {
		return store.Session{}, err
	}
	return session, tx.Commit()
}

func oauthTransactionFromDB(row storedb.OauthTransaction) store.OAuthTransaction {
	return store.OAuthTransaction{
		ID:                 row.ID,
		StateHash:          row.StateHash,
		BrowserBindingHash: row.BrowserBindingHash,
		Mode:               row.Mode,
		PKCEVerifier:       row.PkceVerifier,
		DesktopChallenge:   row.DesktopChallenge,
		DesktopProtocol:    row.DesktopProtocol,
		CreatedAt:          time.Unix(row.CreatedAtUnix, 0).UTC(),
		ExpiresAt:          time.Unix(row.ExpiresAtUnix, 0).UTC(),
	}
}

func validateOAuthTransaction(transaction store.OAuthTransaction) error {
	if transaction.StateHash == "" || transaction.BrowserBindingHash == "" || transaction.PKCEVerifier == "" {
		return store.ErrOAuthTransactionInvalid
	}
	if transaction.Mode != store.OAuthModeBrowser && transaction.Mode != store.OAuthModeDesktop {
		return store.ErrOAuthTransactionInvalid
	}
	if transaction.Mode == store.OAuthModeDesktop && transaction.DesktopChallenge == "" {
		return store.ErrOAuthTransactionInvalid
	}
	if transaction.Mode == store.OAuthModeDesktop && transaction.DesktopProtocol != 1 && transaction.DesktopProtocol != 2 {
		return store.ErrOAuthTransactionInvalid
	}
	if transaction.Mode == store.OAuthModeBrowser && (transaction.DesktopChallenge != "" || transaction.DesktopProtocol != 0) {
		return store.ErrOAuthTransactionInvalid
	}
	if transaction.CreatedAt.IsZero() || !transaction.ExpiresAt.After(transaction.CreatedAt) {
		return store.ErrOAuthTransactionInvalid
	}
	return nil
}

func validateDesktopOAuthGrant(grant store.DesktopOAuthGrant) error {
	if grant.GrantHash == "" || grant.UserID == "" || grant.DesktopChallenge == "" {
		return store.ErrDesktopOAuthGrantInvalid
	}
	if grant.CreatedAt.IsZero() || !grant.ExpiresAt.After(grant.CreatedAt) {
		return store.ErrDesktopOAuthGrantInvalid
	}
	return nil
}
