package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listAppInstallations(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage app installations"))
		return
	}
	installations, err := s.store.ListAppInstallations(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"app_installations": installations}, err)
}

func (s *Server) listEventTypes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.currentActor(r); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"event_types": append([]string(nil), store.DurableEventTypes...),
	})
}

func (s *Server) createAppInstallation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create app installations"))
		return
	}
	var body struct {
		AppSlug     string         `json:"app_slug"`
		DisplayName string         `json:"display_name"`
		BotUserID   string         `json:"bot_user_id"`
		Config      map[string]any `json:"config"`
		SetupNonce  string         `json:"setup_nonce"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	installation, err := s.store.CreateAppInstallation(r.Context(), store.CreateAppInstallationInput{
		WorkspaceID: chi.URLParam(r, "workspace_id"),
		AppSlug:     body.AppSlug,
		DisplayName: body.DisplayName,
		BotUserID:   body.BotUserID,
		Config:      body.Config,
		SetupNonce:  body.SetupNonce,
		CreatedBy:   act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"app_installation": installation}, err)
}

func (s *Server) revokeAppInstallation(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke app installations"))
		return
	}
	options := store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
	}
	var body struct {
		RevokeSlashCommands      *bool `json:"revoke_slash_commands"`
		RevokeEventSubscriptions *bool `json:"revoke_event_subscriptions"`
		RevokeBotTokens          *bool `json:"revoke_bot_tokens"`
		DeleteBot                *bool `json:"delete_bot"`
	}
	if err := readJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.RevokeSlashCommands != nil {
		options.RevokeSlashCommands = *body.RevokeSlashCommands
	}
	if body.RevokeEventSubscriptions != nil {
		options.RevokeEventSubscriptions = *body.RevokeEventSubscriptions
	}
	if body.RevokeBotTokens != nil {
		options.RevokeBotTokens = *body.RevokeBotTokens
	}
	if body.DeleteBot != nil {
		options.DeleteBot = *body.DeleteBot
	}
	result, err := s.store.RevokeAppInstallation(r.Context(), chi.URLParam(r, "installation_id"), act.user.ID, options)
	if err == nil && result.DeletedBot != nil {
		s.publishBotDeleted(*result.DeletedBot)
	}
	writeResult(w, result, err)
}

func (s *Server) listSlashCommands(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage slash commands"))
		return
	}
	commands, err := s.store.ListSlashCommands(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"slash_commands": commands}, err)
}

func (s *Server) createSlashCommand(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create slash commands"))
		return
	}
	var body struct {
		AppInstallationID string `json:"app_installation_id"`
		Command           string `json:"command"`
		Description       string `json:"description"`
		CallbackURL       string `json:"callback_url"`
		BotUserID         string `json:"bot_user_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	command, err := s.store.CreateSlashCommand(r.Context(), store.CreateSlashCommandInput{
		WorkspaceID:       chi.URLParam(r, "workspace_id"),
		AppInstallationID: body.AppInstallationID,
		Command:           body.Command,
		Description:       body.Description,
		CallbackURL:       body.CallbackURL,
		BotUserID:         body.BotUserID,
		CreatedBy:         act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"slash_command": command}, err)
}

func (s *Server) revokeSlashCommand(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke slash commands"))
		return
	}
	command, err := s.store.RevokeSlashCommand(r.Context(), chi.URLParam(r, "command_id"), act.user.ID)
	writeResult(w, map[string]any{"slash_command": command}, err)
}

func (s *Server) rotateSlashCommandSecret(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot rotate slash command secrets"))
		return
	}
	command, err := s.store.RotateSlashCommandSecret(r.Context(), chi.URLParam(r, "command_id"), act.user.ID)
	writeResult(w, map[string]any{"slash_command": command}, err)
}

func (s *Server) listEventSubscriptions(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage event subscriptions"))
		return
	}
	subscriptions, err := s.store.ListEventSubscriptions(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"event_subscriptions": subscriptions}, err)
}

func (s *Server) createEventSubscription(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create event subscriptions"))
		return
	}
	var body struct {
		AppInstallationID string   `json:"app_installation_id"`
		EventTypes        []string `json:"event_types"`
		CallbackURL       string   `json:"callback_url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	subscription, err := s.store.CreateEventSubscription(r.Context(), store.CreateEventSubscriptionInput{
		WorkspaceID:       chi.URLParam(r, "workspace_id"),
		AppInstallationID: body.AppInstallationID,
		EventTypes:        body.EventTypes,
		CallbackURL:       body.CallbackURL,
		CreatedBy:         act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"event_subscription": subscription}, err)
}

func (s *Server) revokeEventSubscription(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke event subscriptions"))
		return
	}
	subscription, err := s.store.RevokeEventSubscription(r.Context(), chi.URLParam(r, "subscription_id"), act.user.ID)
	writeResult(w, map[string]any{"event_subscription": subscription}, err)
}

func (s *Server) rotateEventSubscriptionSecret(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot rotate event subscription secrets"))
		return
	}
	subscription, err := s.store.RotateEventSubscriptionSecret(r.Context(), chi.URLParam(r, "subscription_id"), act.user.ID)
	writeResult(w, map[string]any{"event_subscription": subscription}, err)
}

func (s *Server) listEventDeliveryAttempts(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list event delivery attempts"))
		return
	}
	limit := queryInt(r, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	attempts, err := s.store.ListEventDeliveryAttempts(
		r.Context(),
		chi.URLParam(r, "subscription_id"),
		act.user.ID,
		limit+1,
		r.URL.Query().Get("before"),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var nextCursor *string
	if len(attempts) > limit {
		attempts = attempts[:limit]
		cursor := attempts[len(attempts)-1].ID
		nextCursor = &cursor
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deliveries":  attempts,
		"next_cursor": nextCursor,
	})
}

func (s *Server) listAuditLogEntries(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list audit log entries"))
		return
	}
	entries, err := s.store.ListAuditLogEntries(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID, queryInt(r, "limit", 100))
	writeResult(w, map[string]any{"audit_log_entries": entries}, err)
}

func (s *Server) listConnectedAccounts(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list connected accounts"))
		return
	}
	accounts, err := s.store.ListConnectedAccounts(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"connected_accounts": accounts}, err)
}

func (s *Server) createConnectedAccount(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create connected accounts"))
		return
	}
	var body struct {
		UserID            string         `json:"user_id"`
		Provider          string         `json:"provider"`
		ProviderAccountID string         `json:"provider_account_id"`
		DisplayName       string         `json:"display_name"`
		Scopes            []string       `json:"scopes"`
		Metadata          map[string]any `json:"metadata"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	account, err := s.store.CreateConnectedAccount(r.Context(), store.CreateConnectedAccountInput{
		WorkspaceID:       chi.URLParam(r, "workspace_id"),
		UserID:            body.UserID,
		Provider:          body.Provider,
		ProviderAccountID: body.ProviderAccountID,
		DisplayName:       body.DisplayName,
		Scopes:            body.Scopes,
		Metadata:          body.Metadata,
		CreatedBy:         act.user.ID,
	})
	if err == nil {
		s.recordAudit(r.Context(), account.WorkspaceID, act.user.ID, "connected_account.created", "connected_account", account.ID, map[string]any{"provider": account.Provider})
	}
	writeResultStatus(w, http.StatusCreated, map[string]any{"connected_account": account}, err)
}

func (s *Server) revokeConnectedAccount(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke connected accounts"))
		return
	}
	account, err := s.store.RevokeConnectedAccount(r.Context(), chi.URLParam(r, "account_id"), act.user.ID)
	if err == nil {
		s.recordAudit(r.Context(), account.WorkspaceID, act.user.ID, "connected_account.revoked", "connected_account", account.ID, map[string]any{"provider": account.Provider})
	}
	writeResult(w, map[string]any{"connected_account": account}, err)
}
