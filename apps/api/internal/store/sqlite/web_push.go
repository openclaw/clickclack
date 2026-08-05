package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func normalizeWebPushSubscription(input store.UpsertWebPushSubscriptionInput) (store.WebPushSubscription, error) {
	subscription := input.Subscription
	subscription.Endpoint = strings.TrimSpace(subscription.Endpoint)
	subscription.Keys.P256DH = strings.TrimSpace(subscription.Keys.P256DH)
	subscription.Keys.Auth = strings.TrimSpace(subscription.Keys.Auth)
	if subscription.ExpirationTime != nil {
		value := strings.TrimSpace(*subscription.ExpirationTime)
		if value == "" {
			subscription.ExpirationTime = nil
		} else {
			subscription.ExpirationTime = &value
		}
	}
	if input.UserID == "" {
		return store.WebPushSubscription{}, errors.New("user_id is required")
	}
	if subscription.Endpoint == "" {
		return store.WebPushSubscription{}, errors.New("web push endpoint is required")
	}
	if subscription.Keys.P256DH == "" || subscription.Keys.Auth == "" {
		return store.WebPushSubscription{}, errors.New("web push subscription keys are required")
	}
	return subscription, nil
}

func (s *Store) UpsertWebPushSubscription(ctx context.Context, input store.UpsertWebPushSubscriptionInput) (store.WebPushSubscription, error) {
	subscription, err := normalizeWebPushSubscription(input)
	if err != nil {
		return store.WebPushSubscription{}, err
	}
	timestamp := now()
	var expirationTime any
	if subscription.ExpirationTime != nil {
		expirationTime = *subscription.ExpirationTime
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO web_push_subscriptions (endpoint, user_id, p256dh, auth, expiration_time, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			user_id = excluded.user_id,
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			expiration_time = excluded.expiration_time,
			updated_at = excluded.updated_at
	`, subscription.Endpoint, input.UserID, subscription.Keys.P256DH, subscription.Keys.Auth, expirationTime, timestamp, timestamp)
	if err != nil {
		return store.WebPushSubscription{}, err
	}
	return subscription, nil
}

func (s *Store) DeleteWebPushSubscription(ctx context.Context, userID, endpoint string) error {
	userID = strings.TrimSpace(userID)
	endpoint = strings.TrimSpace(endpoint)
	if userID == "" {
		return errors.New("user_id is required")
	}
	if endpoint == "" {
		return errors.New("endpoint is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM web_push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	return err
}

func (s *Store) ListWebPushSubscriptions(ctx context.Context, userID string) ([]store.WebPushSubscription, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT endpoint, p256dh, auth, expiration_time
		FROM web_push_subscriptions
		WHERE user_id = ?
		ORDER BY updated_at DESC, endpoint
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]store.WebPushSubscription, 0)
	for rows.Next() {
		var subscription store.WebPushSubscription
		var expirationTime sql.NullString
		if err := rows.Scan(&subscription.Endpoint, &subscription.Keys.P256DH, &subscription.Keys.Auth, &expirationTime); err != nil {
			return nil, err
		}
		if expirationTime.Valid {
			value := strings.TrimSpace(expirationTime.String)
			if value != "" {
				subscription.ExpirationTime = &value
			}
		}
		out = append(out, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
