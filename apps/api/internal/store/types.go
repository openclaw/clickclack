package store

import (
	"context"
	"errors"
	"time"
)

// ErrQuotedMessageOutOfScope is returned when a message tries to quote another
// message that does not belong to the same channel, direct conversation, or
// thread. It is surfaced to API callers as a 400.
var ErrQuotedMessageOutOfScope = errors.New("quoted message is not in this channel, conversation, or thread")

// ErrClientNonceConflict is returned when a client reuses an idempotency nonce
// for a different message request.
var ErrClientNonceConflict = errors.New("client nonce was already used for a different message")

// ErrInvalidMessagePage is returned when a message-history request combines
// mutually exclusive cursors or uses an invalid cursor value.
var ErrInvalidMessagePage = errors.New("invalid message page request")

// ErrModerationRestricted is returned when a workspace moderation rule blocks
// a write. HTTP callers surface it as a 403 or 429 depending on the rule.
var ErrModerationRestricted = errors.New("moderation restriction")

// ErrMessageNotWritable is returned when a user can read a message but cannot
// mutate it.
var ErrMessageNotWritable = errors.New("message is not writable")

// ErrPostRateLimited is returned when a waiting-room guest exhausts the small
// daily post budget.
var ErrPostRateLimited = errors.New("waiting room post limit reached")

// ErrUploadQuotaExceeded is returned when a user has exhausted their upload
// budget in a workspace.
var ErrUploadQuotaExceeded = errors.New("upload quota exceeded")

// ErrUploadNonceConflict is returned when a client reuses an upload nonce in
// another workspace.
var ErrUploadNonceConflict = errors.New("upload nonce was already used in another workspace")

// ErrUploadNonceInProgress is returned while another request owns the same
// upload nonce claim and has not committed or released it yet.
var ErrUploadNonceInProgress = errors.New("upload nonce is already in progress")

var (
	ErrOAuthTransactionInvalid  = errors.New("invalid or expired oauth transaction")
	ErrOAuthCapacityExceeded    = errors.New("too many pending oauth requests")
	ErrDesktopOAuthGrantInvalid = errors.New("invalid or expired desktop oauth grant")
)

// ErrInvalidMessageKind is returned when a caller supplies a message kind that
// is not one of the recognised values. HTTP callers surface it as a 400.
var ErrInvalidMessageKind = errors.New("invalid message kind")

// ErrTurnIDNotAllowed is returned when an ordinary ('message') row is created
// with a non-empty turn_id. turn_id correlates a sequence of agent activity
// rows belonging to one turn; an ordinary message carrying one contradicts the
// documented "must be empty for ordinary messages" contract. HTTP callers surface it as
// a 400 so a client bug fails closed instead of silently persisting a
// contradictory turn_id.
var ErrTurnIDNotAllowed = errors.New("turn_id is only valid for agent activity messages")

// Message kinds. 'message' is an ordinary human/bot message and is the default
// for any row created before this column existed. The agent_* kinds are
// durable agent activity rows: they ride the normal message stream (channel
// sequence, message.created fan-out, scrollback) but are excluded from
// full-text search and from unread/notification accounting.
const (
	MessageKindMessage         = "message"
	MessageKindAgentCommentary = "agent_commentary"
	MessageKindAgentTool       = "agent_tool"
)

// AgentActivityWriteScope is the dedicated, non-inherited bot scope required to
// create an agent activity message (kind != 'message'). It is deliberately
// EXCLUDED from the bot:* bundles so existing deployments' capability surface
// is unchanged: a bot must be granted it explicitly.
const AgentActivityWriteScope = "agent_activity:write"

// IsActivityMessageKind reports whether kind is one of the durable agent
// activity kinds (anything other than the ordinary 'message').
func IsActivityMessageKind(kind string) bool {
	return kind == MessageKindAgentCommentary || kind == MessageKindAgentTool
}

// NormalizeMessageKind validates a caller-supplied kind. An empty value
// defaults to 'message'. Unknown values return ErrInvalidMessageKind.
func NormalizeMessageKind(kind string) (string, error) {
	switch kind {
	case "", MessageKindMessage:
		return MessageKindMessage, nil
	case MessageKindAgentCommentary, MessageKindAgentTool:
		return kind, nil
	default:
		return "", ErrInvalidMessageKind
	}
}

// ErrNotWorkspaceManager is returned when a workspace operation requires an
// owner or moderator.
var ErrNotWorkspaceManager = errors.New("workspace manager permission required")

// ErrWorkspaceOwnerRequired is returned when a workspace operation requires the
// current owner, not just a moderator.
var ErrWorkspaceOwnerRequired = errors.New("workspace owner permission required")

// ErrBotOwnerRequired is returned when a user-owned bot operation is attempted
// by someone other than the bot owner.
var ErrBotOwnerRequired = errors.New("only the bot owner can manage this bot")

// ErrBotOwnerMembershipRequired is returned when a user-owned bot operation is
// attempted after the owner has lost membership in that workspace.
var ErrBotOwnerMembershipRequired = errors.New("bot owner must be a workspace member")

// ErrBotOwnerCreateRequired is returned when someone other than the owner tries
// to create a user-owned bot.
var ErrBotOwnerCreateRequired = errors.New("only the bot owner can create a user-owned bot")

const (
	WorkspaceRoleOwner           = "owner"
	WorkspaceRoleModerator       = "moderator"
	WorkspaceRoleMember          = "member"
	WorkspaceRoleGuest           = "guest"
	WorkspaceRoleBot             = "bot"
	GuestChannelName             = "guest"
	GuestPostLimit               = 3
	MaxDirectConversationMembers = 32

	UploadQuotaBytesPerUserWorkspace int64 = 512 << 20
	UploadQuotaCountPerUserWorkspace int64 = 64
)

type User struct {
	ID                   string                `json:"id"`
	Kind                 string                `json:"kind"`
	OwnerUserID          string                `json:"owner_user_id,omitempty"`
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
	RouteID   string `json:"route_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IconURL   string `json:"icon_url"`
	CreatedAt string `json:"created_at"`
	Role      string `json:"role,omitempty"`
}

type Channel struct {
	ID          string  `json:"id"`
	RouteID     string  `json:"route_id"`
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
	ID                   string  `json:"id"`
	RouteID              string  `json:"route_id,omitempty"`
	WorkspaceID          string  `json:"workspace_id"`
	ChannelID            string  `json:"channel_id,omitempty"`
	DirectConversationID string  `json:"direct_conversation_id,omitempty"`
	AuthorID             string  `json:"author_id"`
	ParentMessageID      *string `json:"parent_message_id,omitempty"`
	ThreadRootID         string  `json:"thread_root_id"`
	TopicID              string  `json:"topic_id,omitempty"`
	ChannelSeq           *int64  `json:"channel_seq,omitempty"`
	ThreadSeq            *int64  `json:"thread_seq,omitempty"`
	Body                 string  `json:"body"`
	BodyFormat           string  `json:"body_format"`
	CreatedAt            string  `json:"created_at"`
	EditedAt             *string `json:"edited_at,omitempty"`
	DeletedAt            *string `json:"deleted_at,omitempty"`
	// Kind discriminates ordinary messages from durable agent activity rows.
	// Empty in JSON means the default 'message'.
	Kind string `json:"kind,omitempty"`
	// TurnID correlates a sequence of agent activity rows belonging to one
	// agent turn. It must be empty for ordinary messages (kind="message"): the
	// create path enforces this and rejects a non-empty turn_id on a 'message'
	// kind with a 400 ErrTurnIDNotAllowed. It is optional for agent activity
	// kinds (agent_commentary/agent_tool), which may carry one.
	TurnID             string       `json:"turn_id,omitempty"`
	Author             *User        `json:"author,omitempty"`
	Attachments        []Upload     `json:"attachments,omitempty"`
	QuotedMessageID    *string      `json:"quoted_message_id,omitempty"`
	QuotedBodySnapshot string       `json:"quoted_body_snapshot,omitempty"`
	QuotedAuthorID     *string      `json:"quoted_author_id,omitempty"`
	QuotedAuthor       *User        `json:"quoted_author,omitempty"`
	ThreadState        *ThreadState `json:"thread_state,omitempty"`
	// Nonce is a client-supplied idempotency key used by optimistic UIs to match
	// the server response to a pending placeholder and safely retry after a lost
	// response.
	Nonce string `json:"nonce,omitempty"`
}

type MessagePageRequest struct {
	Limit     int
	BeforeSeq *int64
	AfterSeq  *int64
	AroundSeq *int64
}

type MessagePage struct {
	Messages  []Message `json:"messages"`
	OldestSeq int64     `json:"oldest_seq"`
	NewestSeq int64     `json:"newest_seq"`
	HasOlder  bool      `json:"has_older"`
	HasNewer  bool      `json:"has_newer"`
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

type CreateBotInput struct {
	WorkspaceID string
	OwnerUserID string
	DisplayName string
	Handle      string
	AvatarURL   string
	TokenName   string
	Scopes      []string
	CreatedBy   string
}

type BotToken struct {
	ID          string   `json:"id"`
	BotUserID   string   `json:"bot_user_id"`
	WorkspaceID string   `json:"workspace_id"`
	OwnerUserID string   `json:"owner_user_id,omitempty"`
	Name        string   `json:"name"`
	Scopes      []string `json:"scopes"`
	CreatedBy   string   `json:"created_by,omitempty"`
	CreatedAt   string   `json:"created_at"`
	LastUsedAt  *string  `json:"last_used_at,omitempty"`
	RevokedAt   *string  `json:"revoked_at,omitempty"`
	Token       string   `json:"token,omitempty"`
}

type BotWithTokens struct {
	Bot    User       `json:"bot"`
	Tokens []BotToken `json:"tokens"`
}

type OwnedBotWorkspace struct {
	ID      string `json:"id"`
	RouteID string `json:"route_id"`
	Name    string `json:"name"`
}

type OwnedBotEntry struct {
	Bot              User              `json:"bot"`
	Workspace        OwnedBotWorkspace `json:"workspace"`
	ActiveTokenCount int               `json:"active_token_count"`
}

type CreateBotTokenInput struct {
	WorkspaceID string
	BotUserID   string
	Name        string
	Scopes      []string
	CreatedBy   string
}

type AppInstallation struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	AppSlug     string         `json:"app_slug"`
	DisplayName string         `json:"display_name"`
	BotUserID   string         `json:"bot_user_id"`
	Config      map[string]any `json:"config"`
	CreatedBy   string         `json:"created_by,omitempty"`
	CreatedAt   string         `json:"created_at"`
	RevokedAt   *string        `json:"revoked_at,omitempty"`
}

type CreateAppInstallationInput struct {
	WorkspaceID string
	AppSlug     string
	DisplayName string
	BotUserID   string
	Config      map[string]any
	CreatedBy   string
}

type RevokeAppInstallationOptions struct {
	RevokeSlashCommands      bool
	RevokeEventSubscriptions bool
	RevokeBotTokens          bool
}

type AppInstallationRevokedCounts struct {
	SlashCommands      int `json:"slash_commands"`
	EventSubscriptions int `json:"event_subscriptions"`
	BotTokens          int `json:"bot_tokens"`
}

type RevokeAppInstallationResult struct {
	Installation AppInstallation              `json:"installation"`
	Revoked      AppInstallationRevokedCounts `json:"revoked"`
}

type SlashCommand struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	AppInstallationID string  `json:"app_installation_id,omitempty"`
	Command           string  `json:"command"`
	Description       string  `json:"description"`
	CallbackURL       string  `json:"callback_url"`
	SigningSecret     string  `json:"signing_secret,omitempty"`
	BotUserID         string  `json:"bot_user_id"`
	CreatedBy         string  `json:"created_by,omitempty"`
	CreatedAt         string  `json:"created_at"`
	RevokedAt         *string `json:"revoked_at,omitempty"`
}

type CreateSlashCommandInput struct {
	WorkspaceID       string
	AppInstallationID string
	Command           string
	Description       string
	CallbackURL       string
	BotUserID         string
	CreatedBy         string
}

type SlashCommandInvocation struct {
	ID             string  `json:"id"`
	CommandID      string  `json:"command_id"`
	WorkspaceID    string  `json:"workspace_id"`
	ChannelID      string  `json:"channel_id"`
	UserID         string  `json:"user_id"`
	Text           string  `json:"text"`
	PayloadJSON    string  `json:"payload_json,omitempty"`
	ResponseStatus int     `json:"response_status"`
	ResponseBody   string  `json:"response_body,omitempty"`
	Error          string  `json:"error,omitempty"`
	CreatedAt      string  `json:"created_at"`
	CompletedAt    *string `json:"completed_at,omitempty"`
}

type CreateSlashCommandInvocationInput struct {
	CommandID   string
	WorkspaceID string
	ChannelID   string
	UserID      string
	Text        string
	PayloadJSON string
}

type EventSubscription struct {
	ID                string   `json:"id"`
	WorkspaceID       string   `json:"workspace_id"`
	AppInstallationID string   `json:"app_installation_id,omitempty"`
	EventTypes        []string `json:"event_types"`
	CallbackURL       string   `json:"callback_url"`
	SigningSecret     string   `json:"signing_secret,omitempty"`
	CreatedBy         string   `json:"created_by,omitempty"`
	CreatedAt         string   `json:"created_at"`
	RevokedAt         *string  `json:"revoked_at,omitempty"`
}

type CreateEventSubscriptionInput struct {
	WorkspaceID       string
	AppInstallationID string
	EventTypes        []string
	CallbackURL       string
	CreatedBy         string
}

type EventDeliveryAttempt struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscription_id"`
	EventID        string `json:"event_id"`
	WorkspaceID    string `json:"workspace_id"`
	EventType      string `json:"event_type"`
	Attempt        int    `json:"attempt"`
	RequestJSON    string `json:"request_json,omitempty"`
	ResponseStatus int    `json:"response_status"`
	ResponseBody   string `json:"response_body,omitempty"`
	Error          string `json:"error,omitempty"`
	CreatedAt      string `json:"created_at"`
	CompletedAt    string `json:"completed_at"`
}

type CreateEventDeliveryAttemptInput struct {
	SubscriptionID string
	EventID        string
	WorkspaceID    string
	EventType      string
	RequestJSON    string
	ResponseStatus int
	ResponseBody   string
	Error          string
}

type AuditLogEntry struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	ActorUserID string         `json:"actor_user_id"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type"`
	TargetID    string         `json:"target_id"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   string         `json:"created_at"`
}

type CreateAuditLogEntryInput struct {
	WorkspaceID string
	ActorUserID string
	Action      string
	TargetType  string
	TargetID    string
	Metadata    map[string]any
}

type ConnectedAccount struct {
	ID                string         `json:"id"`
	WorkspaceID       string         `json:"workspace_id"`
	UserID            string         `json:"user_id"`
	Provider          string         `json:"provider"`
	ProviderAccountID string         `json:"provider_account_id"`
	DisplayName       string         `json:"display_name"`
	Scopes            []string       `json:"scopes"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         string         `json:"created_at"`
	RevokedAt         *string        `json:"revoked_at,omitempty"`
}

type CreateConnectedAccountInput struct {
	WorkspaceID       string
	UserID            string
	Provider          string
	ProviderAccountID string
	DisplayName       string
	Scopes            []string
	Metadata          map[string]any
	CreatedBy         string
}

type BotTokenAuth struct {
	User        User
	TokenID     string
	WorkspaceID string
	Scopes      []string
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

type UpdateUserProfileAndNotificationSettingsInput struct {
	UserID               string
	DisplayName          string
	Handle               string
	AvatarURL            string
	NotificationSettings *NotificationSettings
}

type CreateWorkspaceInput struct {
	Name string
	Slug string
}

type UpdateWorkspaceInput struct {
	WorkspaceID string
	ActorUserID string
	Name        *string
	Slug        *string
	IconURL     *string
}

type TransferWorkspaceOwnershipInput struct {
	WorkspaceID    string
	ActorUserID    string
	NewOwnerUserID string
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
	TopicID         string
	// Kind defaults to 'message' when empty. Activity kinds are gated at the
	// API layer by AgentActivityWriteScope.
	Kind   string
	TurnID string
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
	Nonce           string
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
	Nonce       string `json:"nonce,omitempty"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	DurationMS  int    `json:"duration_ms"`
	StoragePath string `json:"-"`
	CreatedAt   string `json:"created_at"`
}

type PendingUploadCleanup struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	StoragePath string `json:"storage_path"`
	Attempts    int64  `json:"attempts"`
	LastError   string `json:"last_error"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateUploadInput struct {
	WorkspaceID string
	OwnerID     string
	Nonce       string
	Filename    string
	ContentType string
	ByteSize    int64
	Width       int
	Height      int
	DurationMS  int
	StoragePath string
}

type UploadQuota struct {
	MaxBytes       int64
	UsedBytes      int64
	RemainingBytes int64
	MaxCount       int64
	UsedCount      int64
	RemainingCount int64
}

func (q UploadQuota) CanFit(byteSize int64) error {
	if q.RemainingCount <= 0 || byteSize > q.RemainingBytes {
		return ErrUploadQuotaExceeded
	}
	return nil
}

type UploadQuotaReservation struct {
	ID          string
	WorkspaceID string
	OwnerID     string
	Nonce       string
	ByteSize    int64
	CreatedAt   string
	ExpiresAt   string
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
	RouteID     string `json:"route_id"`
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
	Kind            string
	TurnID          string
}

type Topic struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ChannelID   string  `json:"channel_id,omitempty"`
	Name        string  `json:"name"`
	CreatedBy   string  `json:"created_by,omitempty"`
	CreatedAt   string  `json:"created_at"`
	ArchivedAt  *string `json:"archived_at,omitempty"`
}

type CreateTopicInput struct {
	WorkspaceID string
	ChannelID   string
	Name        string
	CreatedBy   string
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

const (
	OAuthModeBrowser = "browser"
	OAuthModeDesktop = "desktop"
)

type OAuthTransaction struct {
	ID                 string
	StateHash          string
	BrowserBindingHash string
	Mode               string
	PKCEVerifier       string
	DesktopChallenge   string
	DesktopProtocol    int64
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

type DesktopOAuthGrant struct {
	ID               string
	GrantHash        string
	UserID           string
	DesktopChallenge string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

type ReadReceipt struct {
	ScopeID     string `json:"scope_id"`
	UserID      string `json:"user_id"`
	LastReadSeq int64  `json:"last_read_seq"`
	LastReadAt  string `json:"last_read_at"`
}

type RouteTarget struct {
	WorkspaceID      string `json:"workspace_id"`
	WorkspaceRouteID string `json:"workspace_route_id"`
	TargetType       string `json:"target_type"`
	TargetID         string `json:"target_id"`
	TargetRouteID    string `json:"target_route_id"`
	ParentType       string `json:"parent_type,omitempty"`
	ParentID         string `json:"parent_id,omitempty"`
	ParentRouteID    string `json:"parent_route_id,omitempty"`
	CanonicalPath    string `json:"canonical_path"`
}

type MemberModeration struct {
	WorkspaceID    string  `json:"workspace_id"`
	User           User    `json:"user"`
	Role           string  `json:"role"`
	PostsRemaining int     `json:"posts_remaining"`
	PostLimit      int     `json:"post_limit"`
	TimeoutUntil   *string `json:"timeout_until,omitempty"`
	BlockedAt      *string `json:"blocked_at,omitempty"`
	ModerationNote string  `json:"moderation_note,omitempty"`
	ModerationBy   string  `json:"moderation_by,omitempty"`
	ModerationAt   string  `json:"moderation_at,omitempty"`
}

type UpdateMemberModerationInput struct {
	WorkspaceID    string
	TargetUserID   string
	ActorUserID    string
	Role           string
	TimeoutUntil   *string
	ClearTimeout   bool
	Blocked        *bool
	ModerationNote *string
}

type Store interface {
	Close() error
	Ping(ctx context.Context) error
	Migrate(ctx context.Context) error
	EnsureBootstrap(ctx context.Context, name, email string) (User, error)
	CreateUser(ctx context.Context, input CreateUserInput) (User, error)
	CreateBot(ctx context.Context, input CreateBotInput) (User, BotToken, error)
	ListBots(ctx context.Context, workspaceID, requesterID string) ([]BotWithTokens, error)
	CreateBotToken(ctx context.Context, input CreateBotTokenInput) (BotToken, error)
	ListBotTokens(ctx context.Context, botUserID, requesterID string) ([]BotToken, error)
	ListBotTokensForWorkspace(ctx context.Context, workspaceID, botUserID, requesterID string) ([]BotToken, error)
	RevokeBotToken(ctx context.Context, tokenID, requesterID string) (BotToken, error)
	RemoveBotFromWorkspace(ctx context.Context, workspaceID, botUserID, requesterID string) error
	ListBotsOwnedBy(ctx context.Context, ownerUserID string) ([]OwnedBotEntry, error)
	ListAppInstallations(ctx context.Context, workspaceID, requesterID string) ([]AppInstallation, error)
	CreateAppInstallation(ctx context.Context, input CreateAppInstallationInput) (AppInstallation, error)
	RevokeAppInstallation(ctx context.Context, installationID, requesterID string, options RevokeAppInstallationOptions) (RevokeAppInstallationResult, error)
	ListSlashCommands(ctx context.Context, workspaceID, requesterID string) ([]SlashCommand, error)
	CreateSlashCommand(ctx context.Context, input CreateSlashCommandInput) (SlashCommand, error)
	RevokeSlashCommand(ctx context.Context, commandID, requesterID string) (SlashCommand, error)
	RotateSlashCommandSecret(ctx context.Context, commandID, requesterID string) (SlashCommand, error)
	GetSlashCommandForChannel(ctx context.Context, channelID, command, requesterID string) (SlashCommand, error)
	CreateSlashCommandInvocation(ctx context.Context, input CreateSlashCommandInvocationInput) (SlashCommandInvocation, error)
	CompleteSlashCommandInvocation(ctx context.Context, invocationID string, status int, responseBody, invokeError string) (SlashCommandInvocation, error)
	ListEventSubscriptions(ctx context.Context, workspaceID, requesterID string) ([]EventSubscription, error)
	CreateEventSubscription(ctx context.Context, input CreateEventSubscriptionInput) (EventSubscription, error)
	RevokeEventSubscription(ctx context.Context, subscriptionID, requesterID string) (EventSubscription, error)
	RotateEventSubscriptionSecret(ctx context.Context, subscriptionID, requesterID string) (EventSubscription, error)
	ListEventSubscriptionsForEvent(ctx context.Context, event Event) ([]EventSubscription, error)
	CreateEventDeliveryAttempt(ctx context.Context, input CreateEventDeliveryAttemptInput) (EventDeliveryAttempt, error)
	ListEventDeliveryAttempts(ctx context.Context, subscriptionID, requesterID string, limit int, before string) ([]EventDeliveryAttempt, error)
	CreateAuditLogEntry(ctx context.Context, input CreateAuditLogEntryInput) (AuditLogEntry, error)
	ListAuditLogEntries(ctx context.Context, workspaceID, requesterID string, limit int) ([]AuditLogEntry, error)
	ListConnectedAccounts(ctx context.Context, workspaceID, requesterID string) ([]ConnectedAccount, error)
	CreateConnectedAccount(ctx context.Context, input CreateConnectedAccountInput) (ConnectedAccount, error)
	RevokeConnectedAccount(ctx context.Context, accountID, requesterID string) (ConnectedAccount, error)
	UpsertIdentityUser(ctx context.Context, input UpsertIdentityUserInput) (User, error)
	UpdateUserProfile(ctx context.Context, input UpdateUserProfileInput) (User, error)
	UpdateUserProfileAndNotificationSettings(ctx context.Context, input UpdateUserProfileAndNotificationSettingsInput) (User, error)
	UpdateNotificationSettings(ctx context.Context, input UpdateNotificationSettingsInput) (NotificationSettings, error)
	ListPushNotificationRecipients(ctx context.Context, messageID string) ([]PushNotificationRecipient, error)
	AddWorkspaceMember(ctx context.Context, workspaceID, userID, role string) error
	EnsureDefaultWorkspaceMember(ctx context.Context, userID string) (Workspace, error)
	EnsureDefaultGuestWorkspaceMember(ctx context.Context, userID, role string) (Workspace, error)
	ListWorkspaceMemberPage(ctx context.Context, workspaceID, actorUserID string, page WorkspaceMemberPageRequest) (WorkspaceMemberPage, error)
	ListWorkspaceMembers(ctx context.Context, workspaceID, actorUserID string) ([]MemberModeration, error)
	UpdateMemberModeration(ctx context.Context, input UpdateMemberModerationInput) (MemberModeration, Event, error)
	UserHasNonGuestMembership(ctx context.Context, userID string) (bool, error)
	UploadQuota(ctx context.Context, workspaceID, userID string) (UploadQuota, error)
	CanCreateUpload(ctx context.Context, workspaceID, userID string, byteSize int64) error
	ReserveUploadQuota(ctx context.Context, workspaceID, userID, nonce string, byteSize int64) (UploadQuotaReservation, error)
	CreateReservedUpload(ctx context.Context, reservationID string, input CreateUploadInput) (Upload, error)
	ReleaseUploadQuotaReservation(ctx context.Context, reservationID, userID string) error
	FirstUser(ctx context.Context) (User, error)
	GetUser(ctx context.Context, id string) (User, error)
	ListWorkspaces(ctx context.Context, userID string) ([]Workspace, error)
	CreateWorkspace(ctx context.Context, input CreateWorkspaceInput, ownerID string) (Workspace, error)
	GetWorkspace(ctx context.Context, workspaceID, userID string) (Workspace, error)
	UpdateWorkspace(ctx context.Context, input UpdateWorkspaceInput) (Workspace, Event, error)
	TransferWorkspaceOwnership(ctx context.Context, input TransferWorkspaceOwnershipInput) (Workspace, Event, error)
	DeleteWorkspace(ctx context.Context, workspaceID, actorUserID string) ([]PendingUploadCleanup, error)
	ListPendingUploadCleanups(ctx context.Context, limit int) ([]PendingUploadCleanup, error)
	DeletePendingUploadCleanup(ctx context.Context, cleanupID string) error
	RecordPendingUploadCleanupFailure(ctx context.Context, cleanupID, message string) error
	CanPublishEphemeral(ctx context.Context, workspaceID, channelID, directConversationID, userID string) error
	ResolveRouteTarget(ctx context.Context, userID, workspaceRouteID, targetRouteID string) (RouteTarget, error)
	ResolveLegacyRouteTarget(ctx context.Context, userID, workspaceID, targetID string) (RouteTarget, error)
	ListChannels(ctx context.Context, workspaceID, userID string) ([]Channel, error)
	GetChannel(ctx context.Context, channelID, userID string) (Channel, error)
	CreateChannel(ctx context.Context, input CreateChannelInput) (Channel, Event, error)
	UpdateChannel(ctx context.Context, input UpdateChannelInput) (Channel, Event, error)
	ListTopics(ctx context.Context, workspaceID, requesterID string) ([]Topic, error)
	CreateTopic(ctx context.Context, input CreateTopicInput) (Topic, error)
	ListMessages(ctx context.Context, channelID, userID string, page MessagePageRequest) (MessagePage, error)
	GetMessage(ctx context.Context, messageID, userID string) (Message, error)
	GetMessageByNonce(ctx context.Context, authorID, nonce string) (Message, error)
	EnsureThreadRouteID(ctx context.Context, userID, rootMessageID string) (Message, error)
	CreateMessage(ctx context.Context, input CreateMessageInput) (Message, Event, error)
	UpdateMessage(ctx context.Context, input UpdateMessageInput) (Message, Event, error)
	DeleteMessage(ctx context.Context, input DeleteMessageInput) (Message, Event, error)
	GetThread(ctx context.Context, rootMessageID, userID string, limit int) (Message, []Message, ThreadState, error)
	CreateThreadReply(ctx context.Context, input CreateThreadReplyInput) (Message, ThreadState, []Event, error)
	AddReaction(ctx context.Context, input CreateReactionInput) (Event, error)
	RemoveReaction(ctx context.Context, input CreateReactionInput) (Event, error)
	LatestEventCursor(ctx context.Context, workspaceID, userID string) (string, error)
	ListEventsAfter(ctx context.Context, workspaceID, userID, cursor string, limit int) ([]Event, error)
	CreateUpload(ctx context.Context, input CreateUploadInput) (Upload, error)
	GetUpload(ctx context.Context, uploadID, userID string) (Upload, error)
	GetUploadByNonce(ctx context.Context, ownerID, nonce string) (Upload, error)
	UploadHasDirectMessageAttachment(ctx context.Context, uploadID string) (bool, error)
	UploadHasOtherDirectMessageAttachment(ctx context.Context, uploadID, messageID string) (bool, error)
	AttachUpload(ctx context.Context, input AttachUploadInput) (Event, error)
	SearchMessages(ctx context.Context, workspaceID, channelID, userID, query string, limit int) ([]SearchResult, error)
	ListDirectConversations(ctx context.Context, workspaceID, userID string) ([]DirectConversation, error)
	GetDirectConversation(ctx context.Context, conversationID, userID string) (DirectConversation, error)
	CreateDirectConversation(ctx context.Context, input CreateDirectConversationInput) (DirectConversation, error)
	HideDirectConversation(ctx context.Context, conversationID, userID string) error
	ReopenDirectConversation(ctx context.Context, conversationID, userID string) (DirectConversation, error)
	ListDirectMessages(ctx context.Context, conversationID, userID string, page MessagePageRequest) (MessagePage, error)
	CreateDirectMessage(ctx context.Context, input CreateDirectMessageInput) (Message, Event, error)
	MarkChannelRead(ctx context.Context, channelID, userID string, seq int64) (ReadReceipt, Event, error)
	MarkDirectRead(ctx context.Context, conversationID, userID string, seq int64) (ReadReceipt, Event, error)
	CreateInvite(ctx context.Context, workspaceID, createdBy string) (Invite, error)
	CreateMagicLink(ctx context.Context, email, displayName string) (MagicLink, error)
	ConsumeMagicLink(ctx context.Context, token string) (User, Session, error)
	CreateSession(ctx context.Context, userID string) (Session, error)
	GetSessionUser(ctx context.Context, token string) (User, error)
	CreateOAuthTransaction(ctx context.Context, transaction OAuthTransaction) error
	ConsumeOAuthTransaction(ctx context.Context, stateHash, browserBindingHash string, now time.Time) (OAuthTransaction, error)
	CreateDesktopOAuthGrant(ctx context.Context, grant DesktopOAuthGrant) error
	ConsumeDesktopOAuthGrant(ctx context.Context, grantHash, desktopChallenge string, now time.Time) (Session, error)
	GetBotTokenAuth(ctx context.Context, token string) (BotTokenAuth, error)
}
