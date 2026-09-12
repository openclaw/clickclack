package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage bots"))
		return
	}
	bots, err := s.store.ListBots(r.Context(), chi.URLParam(r, "workspace_id"), act.user.ID)
	writeResult(w, map[string]any{"bots": bots}, err)
}

func (s *Server) listMyBots(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot list owned bots"))
		return
	}
	bots, err := s.store.ListBotsOwnedBy(r.Context(), act.user.ID)
	writeResult(w, map[string]any{"bots": bots}, err)
}

func (s *Server) listBotCommands(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireScope("workspaces:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	commands, err := s.store.ListBotCommands(r.Context(), workspaceID, act.user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, errors.New("workspace membership required"))
		return
	}
	writeResult(w, map[string]any{"bot_commands": commands}, err)
}

func (s *Server) setBotCommands(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID == "" {
		writeError(w, http.StatusForbidden, errors.New("bot token required"))
		return
	}
	if err := act.requireScope(store.BotCommandsWriteScope); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Commands *[]store.BotCommandInput `json:"commands"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Commands == nil {
		writeError(w, http.StatusBadRequest, errors.New("commands is required"))
		return
	}
	commands, err := s.store.SetBotCommands(r.Context(), act.workspaceID, act.user.ID, *body.Commands)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.publishEvent(r.Context(), store.Event{
		Type:        "bot_command.updated",
		WorkspaceID: act.workspaceID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Payload: map[string]string{
			"workspace_id": act.workspaceID,
			"bot_user_id":  act.user.ID,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"bot_commands": commands})
}

func (s *Server) createBot(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create bots"))
		return
	}
	var body struct {
		OwnerUserID  string   `json:"owner_user_id"`
		DisplayName  string   `json:"display_name"`
		Handle       string   `json:"handle"`
		AvatarURL    string   `json:"avatar_url"`
		TokenName    string   `json:"token_name"`
		Scopes       []string `json:"scopes"`
		SetupNonce   string   `json:"setup_nonce"`
		InitialToken *bool    `json:"initial_token"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	skipInitialToken := body.InitialToken != nil && !*body.InitialToken
	bot, token, err := s.store.CreateBot(r.Context(), store.CreateBotInput{
		WorkspaceID:      chi.URLParam(r, "workspace_id"),
		OwnerUserID:      body.OwnerUserID,
		DisplayName:      body.DisplayName,
		Handle:           body.Handle,
		AvatarURL:        body.AvatarURL,
		TokenName:        body.TokenName,
		Scopes:           body.Scopes,
		SetupNonce:       body.SetupNonce,
		CreatedBy:        act.user.ID,
		SkipInitialToken: skipInitialToken,
	})
	result := map[string]any{"bot": bot}
	if !skipInitialToken {
		result["bot_token"] = token
	}
	writeResultStatus(w, http.StatusCreated, result, err)
}

func (s *Server) listBotTokens(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage bot tokens"))
		return
	}
	tokens, err := s.store.ListBotTokens(r.Context(), chi.URLParam(r, "bot_user_id"), act.user.ID)
	writeResult(w, map[string]any{"bot_tokens": tokens}, err)
}

func (s *Server) listWorkspaceBotTokens(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot manage bot tokens"))
		return
	}
	tokens, err := s.store.ListBotTokensForWorkspace(r.Context(), chi.URLParam(r, "workspace_id"), chi.URLParam(r, "bot_user_id"), act.user.ID)
	writeResult(w, map[string]any{"bot_tokens": tokens}, err)
}

func (s *Server) createBotToken(w http.ResponseWriter, r *http.Request) {
	s.createBotTokenForWorkspace(w, r, "")
}

func (s *Server) createWorkspaceBotToken(w http.ResponseWriter, r *http.Request) {
	s.createBotTokenForWorkspace(w, r, chi.URLParam(r, "workspace_id"))
}

func (s *Server) createBotTokenForWorkspace(w http.ResponseWriter, r *http.Request, workspaceID string) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create bot tokens"))
		return
	}
	var body struct {
		Name       string   `json:"name"`
		Scopes     []string `json:"scopes"`
		SetupNonce string   `json:"setup_nonce"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, err := s.store.CreateBotToken(r.Context(), store.CreateBotTokenInput{
		WorkspaceID: workspaceID,
		BotUserID:   chi.URLParam(r, "bot_user_id"),
		Name:        body.Name,
		Scopes:      body.Scopes,
		SetupNonce:  body.SetupNonce,
		CreatedBy:   act.user.ID,
	})
	writeResultStatus(w, http.StatusCreated, map[string]any{"bot_token": token}, err)
}

func (s *Server) createWorkspaceBotSetupCode(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create bot setup codes"))
		return
	}
	var body struct {
		Name     string                     `json:"name"`
		Scopes   []string                   `json:"scopes"`
		Defaults store.BotSetupCodeDefaults `json:"defaults"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	code, err := s.store.CreateBotSetupCode(r.Context(), store.CreateBotSetupCodeInput{
		WorkspaceID: chi.URLParam(r, "workspace_id"),
		BotUserID:   chi.URLParam(r, "bot_user_id"),
		Name:        body.Name,
		Scopes:      body.Scopes,
		Defaults:    body.Defaults,
		CreatedBy:   act.user.ID,
	})
	if err == nil {
		s.recordAudit(r.Context(), code.WorkspaceID, act.user.ID, "bot_setup_code.created", "bot_setup_code", code.ID, map[string]any{
			"bot_user_id": code.BotUserID,
			"token_name":  code.TokenName,
		})
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"setup_code": s.setupCodeContract(r, code)})
}

func (s *Server) claimBotSetupCode(w http.ResponseWriter, r *http.Request) {
	if !s.setupCodeClaimLimiter.allow(clientIPKey(r)) {
		writeError(w, http.StatusTooManyRequests, errors.New("too many setup code attempts, retry later"))
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	claim, err := s.store.ClaimBotSetupCode(r.Context(), body.Code)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.recordAudit(r.Context(), claim.Workspace.ID, claim.Bot.ID, "bot_setup_code.claimed", "bot_token", claim.BotToken.ID, map[string]any{
		"bot_user_id": claim.Bot.ID,
		"token_name":  claim.BotToken.Name,
	})
	response := map[string]any{
		"token": claim.BotToken.Token,
		"bot": map[string]string{
			"id":           claim.Bot.ID,
			"handle":       claim.Bot.Handle,
			"display_name": claim.Bot.DisplayName,
		},
		"workspace": map[string]string{
			"id":       claim.Workspace.ID,
			"route_id": claim.Workspace.RouteID,
			"slug":     claim.Workspace.Slug,
			"name":     claim.Workspace.Name,
		},
		"defaults": claim.Defaults,
	}
	if baseURL := s.apiBaseURL(r); baseURL != "" {
		response["contract_version"] = botSetupContractVersion
		response["api_base_url"] = baseURL
	}
	writeJSON(w, http.StatusOK, response)
}

const botSetupContractVersion = 1

type botSetupCodeContract struct {
	store.BotSetupCode
	ContractVersion int    `json:"contract_version,omitempty"`
	ClaimURL        string `json:"claim_url,omitempty"`
	APIBaseURL      string `json:"api_base_url,omitempty"`
}

func (s *Server) setupCodeContract(r *http.Request, code store.BotSetupCode) botSetupCodeContract {
	response := botSetupCodeContract{BotSetupCode: code}
	if baseURL := s.apiBaseURL(r); baseURL != "" {
		response.ContractVersion = botSetupContractVersion
		response.APIBaseURL = baseURL
		response.ClaimURL = baseURL + "/api/bot-setup-codes/claim"
	}
	return response
}

func (s *Server) removeBotFromWorkspace(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot remove bots from workspaces"))
		return
	}
	err = s.store.RemoveBotFromWorkspace(r.Context(), chi.URLParam(r, "workspace_id"), chi.URLParam(r, "bot_user_id"), act.user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeStoreError(w, err)
		return
	}
	s.publishBotMembershipRemoved(chi.URLParam(r, "workspace_id"), chi.URLParam(r, "bot_user_id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot delete bots"))
		return
	}
	deleted, err := s.store.DeleteBot(r.Context(), chi.URLParam(r, "bot_user_id"), act.user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeStoreError(w, err)
		return
	}
	s.publishBotDeleted(deleted)
	writeJSON(w, http.StatusOK, map[string]any{"deleted_bot": deleted})
}

func (s *Server) revokeBotToken(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot revoke bot tokens"))
		return
	}
	token, err := s.store.RevokeBotToken(r.Context(), chi.URLParam(r, "token_id"), act.user.ID)
	writeResult(w, map[string]any{"bot_token": token}, err)
}

func (s *Server) publishBotDeleted(deleted store.DeletedBot) {
	for _, workspaceID := range deleted.WorkspaceIDs {
		s.hub.Publish(store.Event{
			Type:        "bot.deleted",
			WorkspaceID: workspaceID,
			CreatedAt:   deleted.DeletedAt,
			Payload: map[string]string{
				"bot_user_id":   deleted.ID,
				"display_name":  deleted.DisplayName,
				"former_handle": deleted.FormerHandle,
				"deleted_at":    deleted.DeletedAt,
			},
		})
	}
}

func (s *Server) publishBotMembershipRemoved(workspaceID, botUserID string) {
	s.hub.Publish(store.Event{
		Type:        "bot.membership_removed",
		WorkspaceID: workspaceID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Payload: map[string]string{
			"bot_user_id": botUserID,
		},
	})
}
