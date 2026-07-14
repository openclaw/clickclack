package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func (s *Store) ListEventSubscriptions(ctx context.Context, workspaceID, requesterID string) ([]store.EventSubscription, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, eventSubscriptionSelect(false)+`
		WHERE workspace_id = $1 AND revoked_at IS NULL
		ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventSubscriptions(rows)
}

func (s *Store) CreateEventSubscription(ctx context.Context, input store.CreateEventSubscriptionInput) (store.EventSubscription, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return store.EventSubscription{}, errors.New("workspace_id is required")
	}
	eventTypes, err := normalizeEventTypes(input.EventTypes)
	if err != nil {
		return store.EventSubscription{}, err
	}
	callbackURL, err := normalizeCallbackURL(input.CallbackURL)
	if err != nil {
		return store.EventSubscription{}, err
	}
	eventTypesJSON, err := json.Marshal(eventTypes)
	if err != nil {
		return store.EventSubscription{}, err
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.EventSubscription{}, err
	}
	defer tx.Rollback()
	if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.EventSubscription{}, err
	}
	appInstallationID := strings.TrimSpace(input.AppInstallationID)
	if appInstallationID != "" {
		var one int
		if err := tx.QueryRowContext(ctx, `
			SELECT 1
			FROM app_installations
			WHERE id = $1 AND workspace_id = $2 AND revoked_at IS NULL
			FOR KEY SHARE`, appInstallationID, workspaceID).Scan(&one); err != nil {
			return store.EventSubscription{}, err
		}
	}
	subscription := store.EventSubscription{
		ID:                newID("sub"),
		WorkspaceID:       workspaceID,
		AppInstallationID: appInstallationID,
		EventTypes:        eventTypes,
		CallbackURL:       callbackURL,
		SigningSecret:     newID("ccs"),
		CreatedBy:         createdBy,
		CreatedAt:         now(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO event_subscriptions (id, workspace_id, app_installation_id, event_types_json, callback_url, signing_secret, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		subscription.ID,
		subscription.WorkspaceID,
		sqlOptionalText(subscription.AppInstallationID),
		string(eventTypesJSON),
		subscription.CallbackURL,
		subscription.SigningSecret,
		sqlOptionalText(subscription.CreatedBy),
		subscription.CreatedAt,
	)
	if err != nil {
		return store.EventSubscription{}, err
	}
	return subscription, tx.Commit()
}

func (s *Store) RevokeEventSubscription(ctx context.Context, subscriptionID, requesterID string) (store.EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return store.EventSubscription{}, errors.New("subscription_id is required")
	}
	subscription, err := s.getEventSubscription(ctx, subscriptionID, true)
	if err != nil {
		return store.EventSubscription{}, err
	}
	if err := s.requireWorkspaceManager(ctx, subscription.WorkspaceID, requesterID); err != nil {
		return store.EventSubscription{}, err
	}
	revokedAt := now()
	if _, err := s.db.ExecContext(ctx, `UPDATE event_subscriptions SET revoked_at = COALESCE(revoked_at, $1) WHERE id = $2`, revokedAt, subscriptionID); err != nil {
		return store.EventSubscription{}, err
	}
	return s.getEventSubscription(ctx, subscriptionID, false)
}

func (s *Store) RotateEventSubscriptionSecret(ctx context.Context, subscriptionID, requesterID string) (store.EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return store.EventSubscription{}, errors.New("subscription_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.EventSubscription{}, err
	}
	defer tx.Rollback()
	subscription, err := scanEventSubscription(tx.QueryRowContext(ctx, eventSubscriptionSelect(true)+` WHERE id = $1`, subscriptionID))
	if err != nil {
		return store.EventSubscription{}, err
	}
	if subscription.RevokedAt != nil {
		return store.EventSubscription{}, errors.New("cannot rotate a revoked event subscription")
	}
	if err := requireWorkspaceManagerTx(ctx, tx, subscription.WorkspaceID, requesterID); err != nil {
		return store.EventSubscription{}, err
	}
	secret := newID("ccs")
	result, err := tx.ExecContext(ctx, `UPDATE event_subscriptions SET signing_secret = $1 WHERE id = $2 AND revoked_at IS NULL`, secret, subscriptionID)
	if err != nil {
		return store.EventSubscription{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.EventSubscription{}, err
	}
	if affected != 1 {
		return store.EventSubscription{}, errors.New("cannot rotate a revoked event subscription")
	}
	subscription.SigningSecret = secret
	if err := tx.Commit(); err != nil {
		return store.EventSubscription{}, err
	}
	return subscription, nil
}

func (s *Store) ListEventSubscriptionsForEvent(ctx context.Context, event store.Event) ([]store.EventSubscription, error) {
	if event.ID == "" || event.Cursor == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, eventSubscriptionSelect(true)+`
		WHERE workspace_id = $1 AND revoked_at IS NULL
		ORDER BY created_at`, event.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subscriptions, err := scanEventSubscriptions(rows)
	if err != nil {
		return nil, err
	}
	out := make([]store.EventSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscriptionMatchesEvent(subscription, event.Type) {
			out = append(out, subscription)
		}
	}
	return out, nil
}

func (s *Store) CreateEventDeliveryAttempt(ctx context.Context, input store.CreateEventDeliveryAttemptInput) (store.EventDeliveryAttempt, error) {
	at := now()
	attempt := store.EventDeliveryAttempt{
		ID:             newID("eda"),
		SubscriptionID: strings.TrimSpace(input.SubscriptionID),
		EventID:        strings.TrimSpace(input.EventID),
		WorkspaceID:    strings.TrimSpace(input.WorkspaceID),
		EventType:      strings.TrimSpace(input.EventType),
		Attempt:        1,
		RequestJSON:    input.RequestJSON,
		ResponseStatus: input.ResponseStatus,
		ResponseBody:   input.ResponseBody,
		Error:          input.Error,
		CreatedAt:      at,
		CompletedAt:    at,
	}
	if attempt.SubscriptionID == "" || attempt.EventID == "" || attempt.WorkspaceID == "" {
		return store.EventDeliveryAttempt{}, errors.New("event delivery attempt is incomplete")
	}
	var nextAttempt int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(attempt), 0) + 1
		FROM event_delivery_attempts
		WHERE subscription_id = $1 AND event_id = $2`, attempt.SubscriptionID, attempt.EventID).Scan(&nextAttempt); err != nil {
		return store.EventDeliveryAttempt{}, err
	}
	attempt.Attempt = nextAttempt
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO event_delivery_attempts (id, subscription_id, event_id, workspace_id, event_type, attempt, request_json, response_status, response_body, error, created_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		attempt.ID,
		attempt.SubscriptionID,
		attempt.EventID,
		attempt.WorkspaceID,
		attempt.EventType,
		attempt.Attempt,
		attempt.RequestJSON,
		attempt.ResponseStatus,
		attempt.ResponseBody,
		attempt.Error,
		attempt.CreatedAt,
		attempt.CompletedAt,
	)
	return attempt, err
}

func (s *Store) ListEventDeliveryAttempts(ctx context.Context, subscriptionID, requesterID string, limit int, before string) ([]store.EventDeliveryAttempt, error) {
	subscription, err := s.getEventSubscription(ctx, subscriptionID, false)
	if err != nil {
		return nil, err
	}
	if err := s.requireMembership(ctx, subscription.WorkspaceID, requesterID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 201 {
		limit = 201
	}
	before = strings.TrimSpace(before)
	beforeCreatedAt := ""
	if before != "" {
		beforeCreatedAt, err = s.q.GetEventDeliveryAttemptCursor(ctx, storedb.GetEventDeliveryAttemptCursorParams{
			SubscriptionID: subscriptionID,
			BeforeID:       before,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrInvalidEventDeliveryCursor
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := s.q.ListEventDeliveryAttemptsPage(ctx, storedb.ListEventDeliveryAttemptsPageParams{
		SubscriptionID:  subscriptionID,
		BeforeID:        before,
		BeforeCreatedAt: beforeCreatedAt,
		PageLimit:       int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]store.EventDeliveryAttempt, 0, len(rows))
	for _, row := range rows {
		out = append(out, storeEventDeliveryAttemptFromDB(row))
	}
	return out, nil
}

func (s *Store) getEventSubscription(ctx context.Context, subscriptionID string, includeSecret bool) (store.EventSubscription, error) {
	return scanEventSubscription(s.db.QueryRowContext(ctx, eventSubscriptionSelect(includeSecret)+` WHERE id = $1`, subscriptionID))
}

func normalizeEventTypes(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if value != "*" && !store.IsDurableEventType(value) {
			return nil, fmt.Errorf("unknown event type %q", value)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, errors.New("event_types is required")
	}
	return out, nil
}

func subscriptionMatchesEvent(subscription store.EventSubscription, eventType string) bool {
	for _, value := range subscription.EventTypes {
		if value == "*" || value == eventType {
			return true
		}
	}
	return false
}

func eventSubscriptionSelect(includeSecret bool) string {
	secret := "''"
	if includeSecret {
		secret = "signing_secret"
	}
	return `SELECT id, workspace_id, app_installation_id, event_types_json, callback_url, ` + secret + `, created_by, created_at, revoked_at FROM event_subscriptions`
}

func scanEventSubscriptions(rows *sql.Rows) ([]store.EventSubscription, error) {
	out := []store.EventSubscription{}
	for rows.Next() {
		subscription, err := scanEventSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, subscription)
	}
	return out, rows.Err()
}

func scanEventSubscription(row scanner) (store.EventSubscription, error) {
	var subscription store.EventSubscription
	var appInstallationID, createdBy, revokedAt sql.NullString
	var eventTypesJSON string
	if err := row.Scan(
		&subscription.ID,
		&subscription.WorkspaceID,
		&appInstallationID,
		&eventTypesJSON,
		&subscription.CallbackURL,
		&subscription.SigningSecret,
		&createdBy,
		&subscription.CreatedAt,
		&revokedAt,
	); err != nil {
		return store.EventSubscription{}, err
	}
	if appInstallationID.Valid {
		subscription.AppInstallationID = appInstallationID.String
	}
	if createdBy.Valid {
		subscription.CreatedBy = createdBy.String
	}
	if revokedAt.Valid {
		subscription.RevokedAt = &revokedAt.String
	}
	if err := json.Unmarshal([]byte(eventTypesJSON), &subscription.EventTypes); err != nil {
		return store.EventSubscription{}, err
	}
	return subscription, nil
}
