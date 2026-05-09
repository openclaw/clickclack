package store

import (
	"context"
	"errors"
)

// ErrQuotedMessageOutOfScope is returned when a message tries to quote another
// message that does not belong to the same channel, direct conversation, or
// thread. It is surfaced to API callers as a 400.
var ErrQuotedMessageOutOfScope = errors.New("quoted message is not in this channel, conversation, or thread")

// ErrClientNonceConflict is returned when a client reuses an idempotency nonce
// for a different message request.
var ErrClientNonceConflict = errors.New("client nonce was already used for a different message")

type User struct {
	ID                   string                `json:"id"`
	DisplayName          string                `json:"display_name"`
	Handle               string                `json:"handle"`
	AvatarURL            string                `json:"avatar_url"`
	CreatedAt            string                `json:"created_at"`
	NotificationSettings *NotificationSettings `json:"notification_settings,omitempty"`
}

type NotificationSettings struct {
	PushoverEnabled bool   `json:"pushover_enabled"`
	PushoverUserKey string `json:"pushover_user_key"`
}

type UpdateNotificationSettingsInput struct {
	UserID          string
	PushoverEnabled bool
	PushoverUserKey string
}

type PushNotificationRecipient struct {
	UserID          string
	DisplayName     string
	PushoverUserKey string
}

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
}

type Channel struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	CreatedAt   string  `json:"created_at"`
	ArchivedAt  *string `json:"archived_at,omitempty"`
	LastSeq     int64   `json:"last_seq"`
	LastReadSeq int64   `json:"last_read_seq"`
	UnreadCount int64   `json:"unread_count"`
}

type Message struct {
	ID                   string   `json:"id"`
	WorkspaceID          string   `json:"workspace_id"`
	ChannelID            string   `json:"channel_id,omitempty"`
	DirectConversationID string   `json:"direct_conversation_id,omitempty"`
	AuthorID             string   `json:"author_id"`
	ParentMessageID      *string  `json:"parent_message_id,omitempty"`
	ThreadRootID         string   `json:"thread_root_id"`
	ChannelSeq           *int64   `json:"channel_seq,omitempty"`
	ThreadSeq            *int64   `json:"thread_seq,omitempty"`
	Body                 string   `json:"body"`
	BodyFormat           string   `json:"body_format"`
	CreatedAt            string   `json:"created_at"`
	EditedAt             *string  `json:"edited_at,omitempty"`
	DeletedAt            *string  `json:"deleted_at,omitempty"`
	Author               *User    `json:"author,omitempty"`
	Attachments          []Upload `json:"attachments,omitempty"`
	QuotedMessageID      *string  `json:"quoted_message_id,omitempty"`
	QuotedBodySnapshot   string   `json:"quoted_body_snapshot,omitempty"`
	QuotedAuthorID       *string  `json:"quoted_author_id,omitempty"`
	QuotedAuthor         *User    `json:"quoted_author,omitempty"`
	// Nonce is a client-supplied idempotency key used by optimistic UIs to match
	// the server response to a pending placeholder and safely retry after a lost
	// response.
	Nonce string `json:"nonce,omitempty"`
}

type ThreadState struct {
	RootMessageID          string   `json:"root_message_id"`
	ReplyCount             int64    `json:"reply_count"`
	LastReplyAt            *string  `json:"last_reply_at,omitempty"`
	LastReplyAuthorIDs     []string `json:"last_reply_author_ids"`
	LastReplyAuthorIDsJSON string   `json:"-"`
}

type Event struct {
	ID               string   `json:"id"`
	Cursor           string   `json:"cursor"`
	Type             string   `json:"type"`
	WorkspaceID      string   `json:"workspace_id"`
	ChannelID        string   `json:"channel_id,omitempty"`
	Seq              *int64   `json:"seq,omitempty"`
	CreatedAt        string   `json:"created_at"`
	PayloadJSON      string   `json:"-"`
	Payload          any      `json:"payload"`
	RecipientUserIDs []string `json:"-"`
}

type CreateUserInput struct {
	DisplayName string
	Email       string
}

type UpsertIdentityUserInput struct {
	Provider        string
	ProviderSubject string
	Email           string
	DisplayName     string
	AvatarURL       string
}

type UpdateUserProfileInput struct {
	UserID      string
	DisplayName string
	Handle      string
	AvatarURL   string
}

type CreateWorkspaceInput struct {
	Name string
	Slug string
}

type CreateChannelInput struct {
	WorkspaceID string
	Name        string
	Kind        string
	UserID      string
}

type UpdateChannelInput struct {
	ChannelID string
	UserID    string
	Name      string
	Kind      string
	Archived  *bool
}

type CreateMessageInput struct {
	ChannelID       string
	AuthorID        string
	Body            string
	QuotedMessageID *string
	Nonce           string
}

type UpdateMessageInput struct {
	MessageID string
	UserID    string
	Body      string
}

type DeleteMessageInput struct {
	MessageID string
	UserID    string
}

type CreateThreadReplyInput struct {
	RootMessageID   string
	AuthorID        string
	Body            string
	QuotedMessageID *string
}

type CreateReactionInput struct {
	MessageID string
	UserID    string
	Emoji     string
}

type Upload struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	OwnerID     string `json:"owner_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	DurationMS  int    `json:"duration_ms"`
	StoragePath string `json:"storage_path,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type CreateUploadInput struct {
	WorkspaceID string
	OwnerID     string
	Filename    string
	ContentType string
	ByteSize    int64
	Width       int
	Height      int
	DurationMS  int
	StoragePath string
}

type AttachUploadInput struct {
	MessageID string
	UploadID  string
	UserID    string
}

type SearchResult struct {
	Message Message `json:"message"`
	Rank    float64 `json:"rank"`
}

type DirectConversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	CreatedAt   string `json:"created_at"`
	Members     []User `json:"members"`
	LastSeq     int64  `json:"last_seq"`
	LastReadSeq int64  `json:"last_read_seq"`
	UnreadCount int64  `json:"unread_count"`
}

type CreateDirectConversationInput struct {
	WorkspaceID string
	UserID      string
	MemberIDs   []string
}

type CreateDirectMessageInput struct {
	ConversationID  string
	AuthorID        string
	Body            string
	QuotedMessageID *string
	Nonce           string
}

type Invite struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Token       string  `json:"token"`
	CreatedBy   string  `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	AcceptedAt  *string `json:"accepted_at,omitempty"`
}

type MagicLink struct {
	ID          string  `json:"id"`
	Token       string  `json:"token"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	UsedAt      *string `json:"used_at,omitempty"`
}

type Session struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

type ReadReceipt struct {
	ScopeID     string `json:"scope_id"`
	UserID      string `json:"user_id"`
	LastReadSeq int64  `json:"last_read_seq"`
	LastReadAt  string `json:"last_read_at"`
}

type Store interface {
	Close() error
	Migrate(ctx context.Context) error
	EnsureBootstrap(ctx context.Context, name, email string) (User, error)
	CreateUser(ctx context.Context, input CreateUserInput) (User, error)
	UpsertIdentityUser(ctx context.Context, input UpsertIdentityUserInput) (User, error)
	UpdateUserProfile(ctx context.Context, input UpdateUserProfileInput) (User, error)
	UpdateNotificationSettings(ctx context.Context, input UpdateNotificationSettingsInput) (NotificationSettings, error)
	ListPushNotificationRecipients(ctx context.Context, messageID string) ([]PushNotificationRecipient, error)
	AddWorkspaceMember(ctx context.Context, workspaceID, userID, role string) error
	EnsureDefaultWorkspaceMember(ctx context.Context, userID string) (Workspace, error)
	FirstUser(ctx context.Context) (User, error)
	GetUser(ctx context.Context, id string) (User, error)
	ListWorkspaces(ctx context.Context, userID string) ([]Workspace, error)
	CreateWorkspace(ctx context.Context, input CreateWorkspaceInput, ownerID string) (Workspace, error)
	GetWorkspace(ctx context.Context, workspaceID, userID string) (Workspace, error)
	ListChannels(ctx context.Context, workspaceID, userID string) ([]Channel, error)
	CreateChannel(ctx context.Context, input CreateChannelInput) (Channel, Event, error)
	UpdateChannel(ctx context.Context, input UpdateChannelInput) (Channel, Event, error)
	ListMessages(ctx context.Context, channelID, userID string, afterSeq int64, limit int) ([]Message, error)
	CreateMessage(ctx context.Context, input CreateMessageInput) (Message, Event, error)
	UpdateMessage(ctx context.Context, input UpdateMessageInput) (Message, Event, error)
	DeleteMessage(ctx context.Context, input DeleteMessageInput) (Message, Event, error)
	GetThread(ctx context.Context, rootMessageID, userID string, limit int) (Message, []Message, ThreadState, error)
	CreateThreadReply(ctx context.Context, input CreateThreadReplyInput) (Message, ThreadState, []Event, error)
	AddReaction(ctx context.Context, input CreateReactionInput) (Event, error)
	RemoveReaction(ctx context.Context, input CreateReactionInput) (Event, error)
	ListEventsAfter(ctx context.Context, workspaceID, userID, cursor string, limit int) ([]Event, error)
	CreateUpload(ctx context.Context, input CreateUploadInput) (Upload, error)
	GetUpload(ctx context.Context, uploadID, userID string) (Upload, error)
	AttachUpload(ctx context.Context, input AttachUploadInput) error
	SearchMessages(ctx context.Context, workspaceID, userID, query string, limit int) ([]SearchResult, error)
	ListDirectConversations(ctx context.Context, workspaceID, userID string) ([]DirectConversation, error)
	GetDirectConversation(ctx context.Context, conversationID, userID string) (DirectConversation, error)
	CreateDirectConversation(ctx context.Context, input CreateDirectConversationInput) (DirectConversation, error)
	ListDirectMessages(ctx context.Context, conversationID, userID string, afterSeq int64, limit int) ([]Message, error)
	CreateDirectMessage(ctx context.Context, input CreateDirectMessageInput) (Message, Event, error)
	MarkChannelRead(ctx context.Context, channelID, userID string, seq int64) (ReadReceipt, Event, error)
	MarkDirectRead(ctx context.Context, conversationID, userID string, seq int64) (ReadReceipt, Event, error)
	CreateInvite(ctx context.Context, workspaceID, createdBy string) (Invite, error)
	CreateMagicLink(ctx context.Context, email, displayName string) (MagicLink, error)
	ConsumeMagicLink(ctx context.Context, token string) (User, Session, error)
	CreateSession(ctx context.Context, userID string) (Session, error)
	GetSessionUser(ctx context.Context, token string) (User, error)
}
