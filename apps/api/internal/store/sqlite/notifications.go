package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

var pushoverUserKeyRE = regexp.MustCompile(`^[A-Za-z0-9]{30}$`)

func (s *Store) UpdateNotificationSettings(ctx context.Context, input store.UpdateNotificationSettingsInput) (store.NotificationSettings, error) {
	userKey := strings.TrimSpace(input.PushoverUserKey)
	if input.PushoverEnabled && userKey == "" {
		return store.NotificationSettings{}, errors.New("pushover_user_key is required when pushover notifications are enabled")
	}
	if userKey != "" && !pushoverUserKeyRE.MatchString(userKey) {
		return store.NotificationSettings{}, errors.New("pushover_user_key must be 30 alphanumeric characters")
	}
	enabled := 0
	if input.PushoverEnabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_notification_settings (user_id, pushover_enabled, pushover_user_key)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			pushover_enabled = excluded.pushover_enabled,
			pushover_user_key = excluded.pushover_user_key`, input.UserID, enabled, userKey)
	if err != nil {
		return store.NotificationSettings{}, err
	}
	return store.NotificationSettings{PushoverEnabled: input.PushoverEnabled, PushoverUserKey: userKey}, nil
}

func (s *Store) hydrateUserNotificationSettings(ctx context.Context, user store.User) (store.User, error) {
	settings, err := s.getNotificationSettings(ctx, user.ID)
	if err != nil {
		return store.User{}, err
	}
	user.NotificationSettings = &settings
	return user, nil
}

func (s *Store) getNotificationSettings(ctx context.Context, userID string) (store.NotificationSettings, error) {
	var settings store.NotificationSettings
	var enabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT pushover_enabled, pushover_user_key
		FROM user_notification_settings
		WHERE user_id = ?`, userID).Scan(&enabled, &settings.PushoverUserKey)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return store.NotificationSettings{}, err
	}
	settings.PushoverEnabled = enabled == 1
	return settings, nil
}

func (s *Store) ListPushNotificationRecipients(ctx context.Context, messageID string) ([]store.PushNotificationRecipient, error) {
	message, err := getMessage(ctx, s.db, messageID)
	if err != nil {
		return nil, err
	}
	if message.DirectConversationID != "" {
		return s.listDirectPushNotificationRecipients(ctx, message)
	}
	return s.listWorkspacePushNotificationRecipients(ctx, message)
}

func (s *Store) listWorkspacePushNotificationRecipients(ctx context.Context, message store.Message) ([]store.PushNotificationRecipient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.display_name, uns.pushover_user_key
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		JOIN user_notification_settings uns ON uns.user_id = u.id
		WHERE wm.workspace_id = ?
		  AND u.id <> ?
		  AND uns.pushover_enabled = 1
		  AND uns.pushover_user_key <> ''
		ORDER BY u.id`, message.WorkspaceID, message.AuthorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPushNotificationRecipients(rows)
}

func (s *Store) listDirectPushNotificationRecipients(ctx context.Context, message store.Message) ([]store.PushNotificationRecipient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.display_name, uns.pushover_user_key
		FROM direct_conversation_members dcm
		JOIN users u ON u.id = dcm.user_id
		JOIN user_notification_settings uns ON uns.user_id = u.id
		WHERE dcm.conversation_id = ?
		  AND u.id <> ?
		  AND uns.pushover_enabled = 1
		  AND uns.pushover_user_key <> ''
		ORDER BY u.id`, message.DirectConversationID, message.AuthorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPushNotificationRecipients(rows)
}

func scanPushNotificationRecipients(rows *sql.Rows) ([]store.PushNotificationRecipient, error) {
	out := []store.PushNotificationRecipient{}
	for rows.Next() {
		var recipient store.PushNotificationRecipient
		if err := rows.Scan(&recipient.UserID, &recipient.DisplayName, &recipient.PushoverUserKey); err != nil {
			return nil, err
		}
		out = append(out, recipient)
	}
	return out, rows.Err()
}
