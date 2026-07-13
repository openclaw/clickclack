package sqlite

import (
	"database/sql"
	"encoding/json"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func sqlText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

func sqlOptionalText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func stringFromNull(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func ptrFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullFromPtr(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sqlText(*value)
}

func sqlInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func nullInt64FromPtr(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sqlInt64(*value)
}

func storeUserFromDB(id, kind string, ownerUserID sql.NullString, displayName, handle, avatarURL, createdAt string) store.User {
	return store.User{
		ID:          id,
		Kind:        kind,
		OwnerUserID: stringFromNull(ownerUserID),
		DisplayName: displayName,
		Handle:      handle,
		AvatarURL:   avatarURL,
		CreatedAt:   createdAt,
	}
}

func storeUserFromFirstUser(row storedb.FirstUserRow) store.User {
	return storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
}

func storeUserFromGetUser(row storedb.GetUserRow) store.User {
	return storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
}

func storeUserFromGetSessionUser(row storedb.GetSessionUserRow) store.User {
	user := storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
	user.NotificationSettings = &store.NotificationSettings{
		PushoverEnabled: row.PushoverEnabled != 0,
		PushoverUserKey: row.PushoverUserKey,
	}
	return user
}

func storeUserFromIdentityEmail(row storedb.GetUserByIdentityEmailRow) store.User {
	return storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
}

func storeUserFromIdentityProviderSubject(row storedb.GetUserByIdentityProviderSubjectRow) store.User {
	return storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
}

func storeUserFromDirectConversationMember(row storedb.DirectConversationMembersRow) store.User {
	return storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
}

func storeMagicLinkFromDB(link storedb.AuthMagicLink) store.MagicLink {
	return store.MagicLink{
		ID:          link.ID,
		Token:       link.Token,
		Email:       link.Email,
		DisplayName: link.DisplayName,
		CreatedAt:   link.CreatedAt,
		ExpiresAt:   link.ExpiresAt,
		UsedAt:      ptrFromNull(link.UsedAt),
	}
}

func storeWorkspaceFromListWorkspaces(row storedb.ListWorkspacesRow) store.Workspace {
	return store.Workspace{
		ID:        row.ID,
		RouteID:   row.RouteID,
		Name:      row.Name,
		Slug:      row.Slug,
		IconURL:   row.IconUrl,
		CreatedAt: row.CreatedAt,
		Role:      row.Role,
	}
}

func storeWorkspaceFromGetWorkspace(row storedb.GetWorkspaceRow) store.Workspace {
	return store.Workspace{
		ID:        row.ID,
		RouteID:   row.RouteID,
		Name:      row.Name,
		Slug:      row.Slug,
		IconURL:   row.IconUrl,
		CreatedAt: row.CreatedAt,
		Role:      row.Role,
	}
}

func storeUploadFromGetUpload(row storedb.GetUploadRow) store.Upload {
	return store.Upload{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		OwnerID:     row.OwnerID,
		Nonce:       row.ClientNonce,
		Filename:    row.Filename,
		ContentType: row.ContentType,
		ByteSize:    row.ByteSize,
		Width:       int(row.Width),
		Height:      int(row.Height),
		DurationMS:  int(row.DurationMs),
		StoragePath: row.StoragePath,
		CreatedAt:   row.CreatedAt,
	}
}

func storeUploadFromGetUploadByOwnerNonce(row storedb.GetUploadByOwnerNonceRow) store.Upload {
	return store.Upload{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		OwnerID:     row.OwnerID,
		Nonce:       row.ClientNonce,
		Filename:    row.Filename,
		ContentType: row.ContentType,
		ByteSize:    row.ByteSize,
		Width:       int(row.Width),
		Height:      int(row.Height),
		DurationMS:  int(row.DurationMs),
		StoragePath: row.StoragePath,
		CreatedAt:   row.CreatedAt,
	}
}

func storeChannelFromGetChannel(row storedb.GetChannelRow) store.Channel {
	return store.Channel{
		ID:          row.ID,
		RouteID:     row.RouteID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Kind:        row.Kind,
		CreatedAt:   row.CreatedAt,
		ArchivedAt:  ptrFromNull(row.ArchivedAt),
	}
}

func storeChannelFromListChannels(row storedb.ListChannelsRow) store.Channel {
	return store.Channel{
		ID:          row.ID,
		RouteID:     row.RouteID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Kind:        row.Kind,
		CreatedAt:   row.CreatedAt,
		ArchivedAt:  ptrFromNull(row.ArchivedAt),
		LastSeq:     row.LastSeq,
		LastReadSeq: row.LastReadSeq,
		UnreadCount: row.UnreadCount,
	}
}

func storeNotificationSettingsFromDB(row storedb.GetNotificationSettingsRow) store.NotificationSettings {
	return store.NotificationSettings{
		PushoverEnabled: row.PushoverEnabled == 1,
		PushoverUserKey: row.PushoverUserKey,
	}
}

func storePushRecipient(userID, displayName, userKey string) store.PushNotificationRecipient {
	return store.PushNotificationRecipient{
		UserID:          userID,
		DisplayName:     displayName,
		PushoverUserKey: userKey,
	}
}

func storeBotTokenAuthFromDB(row storedb.GetBotTokenAuthRow) store.BotTokenAuth {
	return store.BotTokenAuth{
		User:        storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt),
		TokenID:     row.TokenID,
		WorkspaceID: row.WorkspaceID,
	}
}

func storeEventFromListEventsAfter(row storedb.ListEventsAfterRow) store.Event {
	event := store.Event{
		ID:          row.ID,
		Cursor:      row.Cursor,
		WorkspaceID: row.WorkspaceID,
		ChannelID:   row.ChannelID,
		Type:        row.Type,
		PayloadJSON: row.PayloadJson,
		CreatedAt:   row.CreatedAt,
	}
	if row.Seq.Valid {
		event.Seq = &row.Seq.Int64
	}
	var payload any
	_ = json.Unmarshal([]byte(event.PayloadJSON), &payload)
	event.Payload = payload
	return event
}

func storeDirectConversationFromList(row storedb.ListDirectConversationsRow) store.DirectConversation {
	return store.DirectConversation{
		ID:          row.ID,
		RouteID:     row.RouteID,
		WorkspaceID: row.WorkspaceID,
		CreatedAt:   row.CreatedAt,
		LastSeq:     row.LastSeq,
		LastReadSeq: row.LastReadSeq,
		UnreadCount: row.UnreadCount,
	}
}

func storeDirectConversationFromGet(row storedb.GetDirectConversationRow) store.DirectConversation {
	return store.DirectConversation{
		ID:          row.ID,
		RouteID:     row.RouteID,
		WorkspaceID: row.WorkspaceID,
		CreatedAt:   row.CreatedAt,
		LastSeq:     row.LastSeq,
		LastReadSeq: row.LastReadSeq,
		UnreadCount: row.UnreadCount,
	}
}

func storeThreadStateFromDB(row storedb.ThreadState) store.ThreadState {
	state := store.ThreadState{
		RootMessageID:          row.RootMessageID,
		ReplyCount:             row.ReplyCount,
		LastReplyAt:            ptrFromNull(row.LastReplyAt),
		LastReplyAuthorIDsJSON: row.LastReplyAuthorIdsJson,
	}
	_ = json.Unmarshal([]byte(state.LastReplyAuthorIDsJSON), &state.LastReplyAuthorIDs)
	return state
}
