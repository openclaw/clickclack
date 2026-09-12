package httpapi

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("workspaces:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	items, err := s.store.ListWorkspaces(r.Context(), act.user.ID)
	if err == nil && act.botTokenID != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.ID == act.workspaceID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeResult(w, map[string]any{"workspaces": items}, err)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot create workspaces"))
		return
	}
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaces, err := s.store.ListWorkspaces(r.Context(), act.user.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hasNonGuestMembership, err := s.store.UserHasNonGuestMembership(r.Context(), act.user.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(workspaces) > 0 && !hasNonGuestMembership {
		writeError(w, http.StatusForbidden, store.ErrModerationRestricted)
		return
	}
	workspace, err := s.store.CreateWorkspace(r.Context(), store.CreateWorkspaceInput{Name: body.Name, Slug: body.Slug}, act.user.ID)
	writeResultStatus(w, http.StatusCreated, map[string]any{"workspace": workspace}, err)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
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
	workspace, err := s.store.GetWorkspace(r.Context(), workspaceID, act.user.ID)
	writeResult(w, map[string]any{"workspace": workspace}, err)
}

func (s *Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot update workspaces"))
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireScope("workspaces:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Name    *string `json:"name"`
		Slug    *string `json:"slug"`
		IconURL *string `json:"icon_url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Name == nil && body.Slug == nil && body.IconURL == nil {
		writeError(w, http.StatusBadRequest, errors.New("workspace update requires at least one field"))
		return
	}
	workspace, event, err := s.store.UpdateWorkspace(r.Context(), store.UpdateWorkspaceInput{
		WorkspaceID: workspaceID,
		ActorUserID: act.user.ID,
		Name:        body.Name,
		Slug:        body.Slug,
		IconURL:     body.IconURL,
	})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
		s.recordAudit(r.Context(), workspaceID, act.user.ID, "workspace.updated", "workspace", workspaceID, map[string]any{"name": workspace.Name, "slug": workspace.Slug, "icon_url": workspace.IconURL})
	}
	writeResult(w, map[string]any{"workspace": workspace, "event": event}, err)
}

func (s *Server) transferWorkspaceOwnership(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot transfer workspace ownership"))
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireScope("workspaces:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspace, event, err := s.store.TransferWorkspaceOwnership(r.Context(), store.TransferWorkspaceOwnershipInput{
		WorkspaceID:    workspaceID,
		ActorUserID:    act.user.ID,
		NewOwnerUserID: body.UserID,
	})
	if err == nil && event.ID != "" {
		s.publishEvent(r.Context(), event)
		s.recordAudit(r.Context(), workspaceID, act.user.ID, "workspace.ownership_transferred", "workspace", workspaceID, map[string]any{"new_owner_user_id": body.UserID})
	}
	writeResult(w, map[string]any{"workspace": workspace, "event": event}, err)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot delete workspaces"))
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireScope("workspaces:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	cleanups, err := s.store.DeleteWorkspace(r.Context(), workspaceID, act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.cleanupUploadObjects(r.Context(), cleanups); err != nil {
		log.Printf("workspace %s deleted with pending upload cleanup retry: %v", workspaceID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listWorkspaceMemberPage(w http.ResponseWriter, r *http.Request) {
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
	page, err := parseWorkspaceMemberPageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	members, err := s.store.ListWorkspaceMemberPage(r.Context(), workspaceID, act.user.ID, page)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func parseWorkspaceMemberPageRequest(r *http.Request) (store.WorkspaceMemberPageRequest, error) {
	values := r.URL.Query()
	page := store.WorkspaceMemberPageRequest{
		Cursor: values.Get("cursor"),
		Query:  values.Get("q"),
		Role:   values.Get("role"),
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		limit, err := strconv.ParseInt(rawLimit, 10, 32)
		if err != nil || limit < 1 {
			return page, fmt.Errorf("%w: limit must be positive", store.ErrInvalidWorkspaceMemberPage)
		}
		page.Limit = int(limit)
	}
	return page, nil
}

func (s *Server) listWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
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
	members, err := s.store.ListWorkspaceMembers(r.Context(), workspaceID, act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (s *Server) updateWorkspaceMemberModeration(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := chi.URLParam(r, "workspace_id")
	if err := act.requireScope("workspaces:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		Role           string  `json:"role"`
		TimeoutUntil   string  `json:"timeout_until"`
		TimeoutMinutes int     `json:"timeout_minutes"`
		ClearTimeout   bool    `json:"clear_timeout"`
		Blocked        *bool   `json:"blocked"`
		ModerationNote *string `json:"moderation_note"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	timeoutUntil := optionalString(body.TimeoutUntil)
	if timeoutUntil == nil && body.TimeoutMinutes > 0 {
		value := time.Now().Add(time.Duration(body.TimeoutMinutes) * time.Minute).UTC().Format(time.RFC3339Nano)
		timeoutUntil = &value
	}
	member, event, err := s.store.UpdateMemberModeration(r.Context(), store.UpdateMemberModerationInput{
		WorkspaceID:    workspaceID,
		TargetUserID:   chi.URLParam(r, "user_id"),
		ActorUserID:    act.user.ID,
		Role:           body.Role,
		TimeoutUntil:   timeoutUntil,
		ClearTimeout:   body.ClearTimeout,
		Blocked:        body.Blocked,
		ModerationNote: body.ModerationNote,
	})
	if err == nil && event.ID != "" {
		s.hub.Publish(event)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": member, "event": event})
}
