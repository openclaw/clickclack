package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	pageRequest, err := parseSearchPageRequest(r, act.user.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(pageRequest.DirectConversationID) != "" {
		if err := act.requireScope("dms:read"); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
	}
	page, err := s.store.SearchMessagePage(r.Context(), pageRequest)
	writeResult(w, page, err)
}

func parseSearchPageRequest(r *http.Request, userID string) (store.SearchPageRequest, error) {
	values := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.ParseInt(rawLimit, 10, 32)
		if err != nil {
			return store.SearchPageRequest{}, fmt.Errorf("%w: limit must be an integer", store.ErrInvalidSearch)
		}
		if parsed <= 0 {
			return store.SearchPageRequest{}, fmt.Errorf("%w: limit must be positive", store.ErrInvalidSearch)
		}
		limit = int(parsed)
	}
	return store.SearchPageRequest{
		WorkspaceID:          values.Get("workspace_id"),
		ChannelID:            values.Get("channel_id"),
		DirectConversationID: values.Get("direct_conversation_id"),
		UserID:               userID,
		Query:                values.Get("q"),
		Sort:                 store.SearchSort(values.Get("sort")),
		Limit:                limit,
		Cursor:               values.Get("cursor"),
	}, nil
}
