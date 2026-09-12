package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) resolveRoute(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceRouteID := chi.URLParam(r, "workspace_route_id")
	targetRouteID := chi.URLParam(r, "target_route_id")
	scope := routeScopeForParam(targetRouteID)
	if scope == "" {
		writeError(w, http.StatusNotFound, errors.New("route not found"))
		return
	}
	if err := act.requireScope(scope); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	var target store.RouteTarget
	if isLegacyRouteParam(workspaceRouteID) || isLegacyRouteParam(targetRouteID) {
		target, err = s.store.ResolveLegacyRouteTarget(r.Context(), act.user.ID, workspaceRouteID, targetRouteID)
	} else {
		target, err = s.store.ResolveRouteTarget(r.Context(), act.user.ID, workspaceRouteID, targetRouteID)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("route not found"))
		return
	}
	if err := act.requireWorkspace(target.WorkspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if scope != routeScopeForTargetType(target.TargetType) {
		writeError(w, http.StatusNotFound, errors.New("route not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"route": target})
}

func routeScopeForParam(value string) string {
	switch {
	case strings.HasPrefix(value, "C"), strings.HasPrefix(value, "chn_"):
		return "channels:read"
	case strings.HasPrefix(value, "D"), strings.HasPrefix(value, "dm_"):
		return "dms:read"
	case strings.HasPrefix(value, "M"), strings.HasPrefix(value, "msg_"):
		return "threads:read"
	default:
		return ""
	}
}

func routeScopeForTargetType(targetType string) string {
	switch targetType {
	case "channel":
		return "channels:read"
	case "direct":
		return "dms:read"
	case "thread":
		return "threads:read"
	default:
		return ""
	}
}

func isLegacyRouteParam(value string) bool {
	return strings.HasPrefix(value, "wsp_") ||
		strings.HasPrefix(value, "chn_") ||
		strings.HasPrefix(value, "dm_") ||
		strings.HasPrefix(value, "msg_")
}
