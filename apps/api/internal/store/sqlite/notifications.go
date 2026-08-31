package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

var pushoverUserKeyRE = regexp.MustCompile(`^[A-Za-z0-9]{30}$`)

func normalizeNotificationSettings(input store.UpdateNotificationSettingsInput) (store.NotificationSettings, int64, error) {
	userKey := strings.TrimSpace(input.PushoverUserKey)
	if input.PushoverEnabled && userKey == "" {
		return store.NotificationSettings{}, 0, errors.New("pushover_user_key is required when pushover notifications are enabled")
	}
	if userKey != "" && !pushoverUserKeyRE.MatchString(userKey) {
		return store.NotificationSettings{}, 0, errors.New("pushover_user_key must be 30 alphanumeric characters")
	}
	var enabled int64
	if input.PushoverEnabled {
		enabled = 1
	}
	return store.NotificationSettings{PushoverEnabled: input.PushoverEnabled, PushoverUserKey: userKey}, enabled, nil
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
	row, err := s.q.GetNotificationSettings(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.NotificationSettings{}, nil
	}
	if err != nil {
		return store.NotificationSettings{}, err
	}
	return storeNotificationSettingsFromDB(row), nil
}

func (s *Store) UpsertChannelNotificationSettings(ctx context.Context, input store.ChannelNotificationInput) error {
	preference, err := normalizeChannelNotificationPreference(input.Preference)
	if err != nil {
		return err
	}
	if _, err := s.GetChannel(ctx, input.ChannelID, input.UserID); err != nil {
		return err
	}
	timestamp := now()
	return s.q.UpsertChannelNotificationSettings(ctx, storedb.UpsertChannelNotificationSettingsParams{
		ChannelID:  input.ChannelID,
		UserID:     input.UserID,
		Preference: preference,
		CreatedAt:  timestamp,
		UpdatedAt:  timestamp,
	})
}

func (s *Store) GetChannelNotificationPreference(ctx context.Context, channelID, userID string) (string, error) {
	if _, err := s.GetChannel(ctx, channelID, userID); err != nil {
		return "", err
	}
	preference, err := s.q.GetChannelNotificationPreference(ctx, storedb.GetChannelNotificationPreferenceParams{
		ChannelID: channelID,
		UserID:    userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return store.ChannelNotifyAll, nil
	}
	return preference, err
}

func normalizeChannelNotificationPreference(preference string) (string, error) {
	switch strings.TrimSpace(preference) {
	case store.ChannelNotifyAll:
		return store.ChannelNotifyAll, nil
	case store.ChannelNotifyMentions:
		return store.ChannelNotifyMentions, nil
	case store.ChannelNotifyMuted:
		return store.ChannelNotifyMuted, nil
	default:
		return "", errors.New("invalid channel notification preference")
	}
}

func mentionedUserIDs(ctx context.Context, queries *storedb.Queries, workspaceID, body string) ([]string, error) {
	handles := store.ParseMessageMentions(body)
	if len(handles) == 0 {
		return nil, nil
	}
	handlesJSON, err := json.Marshal(handles)
	if err != nil {
		return nil, err
	}
	rows, err := queries.ListMentionedUserIDs(ctx, storedb.ListMentionedUserIDsParams{
		WorkspaceID: workspaceID,
		HandlesJson: string(handlesJSON),
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (s *Store) ListPushNotificationRecipients(ctx context.Context, messageID string, mentionedUserIDs []string) ([]store.PushNotificationRecipient, error) {
	message, err := getMessage(ctx, s.db, messageID)
	if err != nil {
		return nil, err
	}
	if message.DirectConversationID != "" {
		return s.listDirectPushNotificationRecipients(ctx, message)
	}
	return s.listWorkspacePushNotificationRecipients(ctx, message, mentionedUserIDs)
}

func (s *Store) listWorkspacePushNotificationRecipients(ctx context.Context, message store.Message, mentionedUserIDs []string) ([]store.PushNotificationRecipient, error) {
	rows, err := s.q.ListWorkspacePushNotificationRecipients(ctx, storedb.ListWorkspacePushNotificationRecipientsParams{
		ChannelID:   message.ChannelID,
		WorkspaceID: message.WorkspaceID,
		AuthorID:    message.AuthorID,
	})
	if err != nil {
		return nil, err
	}
	mentioned := make(map[string]struct{}, len(mentionedUserIDs))
	for _, userID := range mentionedUserIDs {
		mentioned[userID] = struct{}{}
	}
	out := make([]store.PushNotificationRecipient, 0, len(rows))
	for _, row := range rows {
		if row.NotificationPreference == store.ChannelNotifyMuted {
			continue
		}
		if row.NotificationPreference == store.ChannelNotifyMentions {
			if _, ok := mentioned[row.UserID]; !ok {
				continue
			}
		}
		out = append(out, storePushRecipient(row.UserID, row.DisplayName, row.PushoverUserKey))
	}
	return out, nil
}

func (s *Store) listDirectPushNotificationRecipients(ctx context.Context, message store.Message) ([]store.PushNotificationRecipient, error) {
	rows, err := s.q.ListDirectPushNotificationRecipients(ctx, storedb.ListDirectPushNotificationRecipientsParams{
		ConversationID: message.DirectConversationID,
		AuthorID:       message.AuthorID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]store.PushNotificationRecipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, storePushRecipient(row.UserID, row.DisplayName, row.PushoverUserKey))
	}
	return out, nil
}
