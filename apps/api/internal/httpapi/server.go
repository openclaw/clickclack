package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/openclaw/clickclack/apps/api/internal/authpolicy"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/uploadstore"
)

type Server struct {
	store                 store.Store
	hub                   *realtime.Hub
	uploadStorage         uploadstore.Store
	githubOAuth           GitHubOAuthConfig
	openclawID            OpenClawIDConfig
	access                *accessVerifier
	frontendURL           string
	homeLinkConfig        HomeLinkConfig
	publicAPIURL          string
	embedFrameAncestors   []string
	cookies               authpolicy.CookieNames
	cookieSameSite        http.SameSite
	disableDevAuth        bool
	passwordAuthEnabled   bool
	pushNotifier          PushNotifier
	metrics               *metricsRegistry
	accessLog             AccessLogMode
	build                 buildMetadata
	setupCodeClaimLimiter *slidingWindowLimiter
	passwordIPLimiter     *slidingWindowLimiter
	passwordIDLimiter     *slidingWindowLimiter
	passwordChangeLimiter *slidingWindowLimiter
	realtimeReplayLimit   int
	realtimeSessionCheck  time.Duration
	callbackClient        *http.Client
}

const (
	websocketBearerProtocolPrefix     = "clickclack.bearer."
	csrfHeaderName                    = "X-ClickClack-CSRF"
	maxJSONBodyBytes                  = 1 << 20
	readHeaderTimeout                 = 5 * time.Second
	httpRequestTimeout                = 30 * time.Second
	idleTimeout                       = 120 * time.Second
	uploadCleanupSweepLimit           = 100
	realtimeReplayPageSize            = 500
	realtimeReplayMaxEvents           = 5000
	realtimeResyncRequiredStatus      = websocket.StatusCode(4001)
	realtimeOverflowCloseReason       = "realtime buffer overflow; reconnect with after_cursor to replay"
	realtimeReplayCloseReason         = "realtime replay interrupted; reconnect with after_cursor"
	realtimeResyncRequiredCloseReason = "realtime replay limit exceeded; resync required"
	realtimeSessionRevokedCloseReason = "session revoked; sign in again"
	// realtimeSessionRecheckInterval bounds how long a connection whose credential
	// was revoked can sit open while its workspace is quiet. Delivery revalidates
	// the credential regardless, so this only shortens the idle case.
	realtimeSessionRecheckInterval = 30 * time.Second
	// setupCodeClaimLimit/Window bound unauthenticated bot setup code
	// claim attempts per client IP.
	setupCodeClaimLimit  = 10
	setupCodeClaimWindow = time.Minute
	// passwordLoginIPLimit/Window bound password attempts from one client, and
	// passwordLoginIDLimit/Window bound attempts against one account no matter
	// how many addresses they come from. The account window is deliberately
	// long: it is the lockout that makes online guessing impractical.
	passwordLoginIPLimit  = 20
	passwordLoginIPWindow = time.Minute
	passwordLoginIDLimit  = 5
	passwordLoginIDWindow = 15 * time.Minute
	// passwordChangeLimit/Window bound wrong current-password guesses against
	// one signed-in account, so a borrowed session cannot be used to search for
	// the password it is already holding a session for.
	passwordChangeLimit  = 5
	passwordChangeWindow = 15 * time.Minute
)

type Options struct {
	UploadDir           string
	UploadStorage       uploadstore.Store
	GitHubOAuth         GitHubOAuthConfig
	OpenClawID          OpenClawIDConfig
	Access              AccessConfig
	FrontendURL         string
	PublicAPIURL        string
	HomeLink            HomeLinkConfig
	EmbedFrameAncestors []string
	CookieNames         authpolicy.CookieNames
	DisableDevAuth      bool
	PasswordAuthEnabled bool
	PushNotifier        PushNotifier
	MetricsEnabled      bool
	AccessLog           AccessLogMode
	Environment         string
	Version             string
	Commit              string
	callbackClient      *http.Client
}

func New(st store.Store, hub *realtime.Hub, options Options) *Server {
	uploadStorage := options.UploadStorage
	if uploadStorage == nil && options.UploadDir != "" {
		uploadStorage = uploadstore.NewLocal(options.UploadDir)
	}
	var metrics *metricsRegistry
	if options.MetricsEnabled {
		metrics = newMetricsRegistry()
	}
	cookieNames := options.CookieNames
	if cookieNames.Session == "" {
		cookieNames = authpolicy.DefaultCookieNames()
	}
	callbackClient := options.callbackClient
	if callbackClient == nil {
		callbackClient = newCallbackHTTPClient()
	}
	return &Server{
		store:                 st,
		hub:                   hub,
		uploadStorage:         uploadStorage,
		githubOAuth:           options.GitHubOAuth.withDefaults(),
		openclawID:            options.OpenClawID.withDefaults(),
		access:                newAccessVerifier(options.Access),
		frontendURL:           strings.TrimSpace(options.FrontendURL),
		homeLinkConfig:        options.HomeLink.withDefaults(),
		publicAPIURL:          strings.TrimRight(strings.TrimSpace(options.PublicAPIURL), "/"),
		embedFrameAncestors:   append([]string(nil), options.EmbedFrameAncestors...),
		cookies:               cookieNames,
		cookieSameSite:        configuredCookieSameSite(options.FrontendURL, options.PublicAPIURL),
		disableDevAuth:        options.DisableDevAuth,
		passwordAuthEnabled:   options.PasswordAuthEnabled,
		pushNotifier:          options.PushNotifier,
		metrics:               metrics,
		accessLog:             options.AccessLog,
		setupCodeClaimLimiter: newSlidingWindowLimiter(setupCodeClaimLimit, setupCodeClaimWindow),
		passwordIPLimiter:     newSlidingWindowLimiter(passwordLoginIPLimit, passwordLoginIPWindow),
		passwordIDLimiter:     newSlidingWindowLimiter(passwordLoginIDLimit, passwordLoginIDWindow),
		passwordChangeLimiter: newSlidingWindowLimiter(passwordChangeLimit, passwordChangeWindow),
		realtimeReplayLimit:   realtimeReplayMaxEvents,
		realtimeSessionCheck:  realtimeSessionRecheckInterval,
		callbackClient:        callbackClient,
		build: buildMetadata{
			Environment: options.Environment,
			Version:     options.Version,
			Commit:      options.Commit,
		},
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(correlationIDMiddleware)
	if s.metrics != nil {
		r.Use(s.metrics.middleware)
	}
	r.Use(middleware.RequestLogger(&pathOnlyLogFormatter{Mode: s.accessLog}))
	r.Use(middleware.Recoverer)
	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Get("/metrics", s.metricsHandler)

	r.Route("/api", func(r chi.Router) {
		r.Use(s.cors)
		r.Use(s.requireCookieCSRF)
		r.Use(bindAccessResponseWriter)
		r.Post("/auth/magic/request", s.requestMagicLink)
		r.Post("/auth/magic/consume", s.consumeMagicLink)
		r.Post("/auth/password/login", s.passwordLogin)
		r.Post("/auth/password/change", s.changePassword)
		r.Post("/auth/logout", s.logout)
		r.Get("/auth/github/start", s.githubStart)
		r.Get("/auth/github/desktop/start", s.githubDesktopStart)
		r.Post("/auth/github/desktop/consume", s.githubDesktopConsume)
		r.Get("/auth/github/callback", s.githubCallback)
		r.Get("/auth/openclaw/start", s.openclawIDStart)
		r.Get("/auth/openclaw/callback", s.openclawIDCallback)
		r.Get("/home-link", s.homeLink)
		r.Get("/me", s.me)
		r.Patch("/me", s.updateMe)
		r.Get("/me/bots", s.listMyBots)
		r.Get("/event-types", s.listEventTypes)
		r.Get("/workspaces", s.listWorkspaces)
		r.Post("/workspaces", s.createWorkspace)
		r.Get("/routes/{workspace_route_id}/{target_route_id}", s.resolveRoute)
		r.Get("/workspaces/{workspace_id}", s.getWorkspace)
		r.Patch("/workspaces/{workspace_id}", s.updateWorkspace)
		r.Post("/workspaces/{workspace_id}/transfer-ownership", s.transferWorkspaceOwnership)
		r.Delete("/workspaces/{workspace_id}", s.deleteWorkspace)
		r.Get("/workspaces/{workspace_id}/members", s.listWorkspaceMemberPage)
		r.Get("/workspaces/{workspace_id}/moderation/members", s.listWorkspaceMembers)
		r.Patch("/workspaces/{workspace_id}/moderation/members/{user_id}", s.updateWorkspaceMemberModeration)
		r.Get("/workspaces/{workspace_id}/channels", s.listChannels)
		r.Post("/workspaces/{workspace_id}/channels", s.createChannel)
		r.Get("/workspaces/{workspace_id}/topics", s.listTopics)
		r.Post("/workspaces/{workspace_id}/topics", s.createTopic)
		r.Get("/workspaces/{workspace_id}/bots", s.listBots)
		r.Post("/workspaces/{workspace_id}/bots", s.createBot)
		r.Get("/workspaces/{workspace_id}/bot-commands", s.listBotCommands)
		r.Delete("/workspaces/{workspace_id}/bots/{bot_user_id}/membership", s.removeBotFromWorkspace)
		r.Get("/workspaces/{workspace_id}/bots/{bot_user_id}/tokens", s.listWorkspaceBotTokens)
		r.Post("/workspaces/{workspace_id}/bots/{bot_user_id}/tokens", s.createWorkspaceBotToken)
		r.Post("/workspaces/{workspace_id}/bots/{bot_user_id}/setup-codes", s.createWorkspaceBotSetupCode)
		r.Post("/bot-setup-codes/claim", s.claimBotSetupCode)
		r.Put("/bots/self/commands", s.setBotCommands)
		r.Delete("/bots/{bot_user_id}", s.deleteBot)
		r.Get("/bots/{bot_user_id}/tokens", s.listBotTokens)
		r.Post("/bots/{bot_user_id}/tokens", s.createBotToken)
		r.Post("/bot-tokens/{token_id}/revoke", s.revokeBotToken)
		r.Get("/workspaces/{workspace_id}/app-installations", s.listAppInstallations)
		r.Post("/workspaces/{workspace_id}/app-installations", s.createAppInstallation)
		r.Post("/app-installations/{installation_id}/revoke", s.revokeAppInstallation)
		r.Get("/workspaces/{workspace_id}/slash-commands", s.listSlashCommands)
		r.Post("/workspaces/{workspace_id}/slash-commands", s.createSlashCommand)
		r.Post("/slash-commands/{command_id}/revoke", s.revokeSlashCommand)
		r.Post("/slash-commands/{command_id}/rotate-secret", s.rotateSlashCommandSecret)
		r.Get("/workspaces/{workspace_id}/event-subscriptions", s.listEventSubscriptions)
		r.Post("/workspaces/{workspace_id}/event-subscriptions", s.createEventSubscription)
		r.Post("/event-subscriptions/{subscription_id}/revoke", s.revokeEventSubscription)
		r.Post("/event-subscriptions/{subscription_id}/rotate-secret", s.rotateEventSubscriptionSecret)
		r.Get("/event-subscriptions/{subscription_id}/deliveries", s.listEventDeliveryAttempts)
		r.Get("/workspaces/{workspace_id}/audit-log", s.listAuditLogEntries)
		r.Get("/workspaces/{workspace_id}/connected-accounts", s.listConnectedAccounts)
		r.Post("/workspaces/{workspace_id}/connected-accounts", s.createConnectedAccount)
		r.Post("/connected-accounts/{account_id}/revoke", s.revokeConnectedAccount)
		r.Patch("/channels/{channel_id}", s.updateChannel)
		r.Get("/channels/{channel_id}/messages", s.listMessages)
		r.Post("/channels/{channel_id}/messages", s.createMessage)
		r.Get("/channels/{channel_id}/notification-settings", s.getChannelNotificationSettings)
		r.Patch("/channels/{channel_id}/notification-settings", s.updateChannelNotificationSettings)
		r.Get("/channels/{channel_id}/pins", s.listPinnedMessages)
		r.Post("/channels/{channel_id}/pins", s.pinMessage)
		r.Delete("/channels/{channel_id}/pins/{message_id}", s.unpinMessage)
		r.Post("/channels/{channel_id}/read", s.markChannelRead)
		r.Get("/messages/by-nonce", s.getMessageByNonce)
		r.Get("/messages/{message_id}", s.getMessage)
		r.Patch("/messages/{message_id}", s.updateMessage)
		r.Delete("/messages/{message_id}", s.deleteMessage)
		r.Post("/messages/{message_id}/route", s.ensureMessageRoute)
		r.Get("/messages/{message_id}/thread", s.getThread)
		r.Post("/messages/{message_id}/thread/replies", s.createThreadReply)
		r.Post("/messages/{message_id}/reactions", s.addReaction)
		r.Delete("/messages/{message_id}/reactions/{emoji}", s.removeReaction)
		r.Get("/realtime/events", s.listEvents)
		r.Post("/realtime/ephemeral", s.publishEphemeral)
		r.Get("/realtime/ws", s.websocket)
		r.Get("/search", s.search)
		r.Post("/uploads", s.createUpload)
		r.Get("/uploads/by-nonce", s.getUploadByNonce)
		r.Get("/uploads/{upload_id}", s.getUpload)
		r.Post("/messages/{message_id}/attachments", s.attachUpload)
		r.Get("/dms", s.listDirectConversations)
		r.Post("/dms", s.createDirectConversation)
		r.Get("/dms/{conversation_id}", s.getDirectConversation)
		r.Delete("/dms/{conversation_id}", s.hideDirectConversation)
		r.Post("/dms/{conversation_id}/open", s.reopenDirectConversation)
		r.Get("/dms/{conversation_id}/messages", s.listDirectMessages)
		r.Post("/dms/{conversation_id}/messages", s.createDirectMessage)
		r.Post("/dms/{conversation_id}/read", s.markDirectRead)
		r.Post("/hooks/mattermost/{channel_id}", s.mattermostWebhook)
		r.Post("/hooks/slash/{channel_id}", s.slashCommand)
	})

	r.NotFound(s.serveSPA)
	r.Head("/*", s.serveSPA)
	r.Get("/*", s.serveSPA)
	return r
}

func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	server := newHTTPServer(addr, handler)
	defer server.Close()
	shutdownDone := make(chan error, 1)
	stopShutdown := context.AfterFunc(ctx, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(shutdownCtx)
	})
	defer stopShutdown()
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		// Closing the listener returns before active requests finish. Keep the
		// caller and its store alive until draining completes or times out.
		return <-shutdownDone
	}
	return fmt.Errorf("serve %s: %w", addr, err)
}

func withHTTPDeadlines(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Go uses the read side to detect disconnects on bodyless requests;
		// a body deadline there would also cancel a progressing response.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Body == nil || r.Body == http.NoBody {
			handler.ServeHTTP(w, r)
			return
		}
		controller := http.NewResponseController(w)
		deadline := time.Now().Add(httpRequestTimeout)
		_ = controller.SetReadDeadline(deadline)
		defer func() {
			_ = controller.SetReadDeadline(time.Time{})
		}()
		handler.ServeHTTP(w, r)
	})
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           withHTTPDeadlines(handler),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
}
