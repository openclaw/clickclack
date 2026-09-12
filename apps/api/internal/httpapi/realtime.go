package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if err := act.requireScope("realtime:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	result := map[string]any{}
	if r.URL.Query().Get("include_tail") == "true" {
		tailCursor, err := s.store.LatestEventCursor(r.Context(), workspaceID, act.user.ID)
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		result["tail_cursor"] = tailCursor
	}
	events, err := s.store.ListEventsAfter(r.Context(), workspaceID, act.user.ID, r.URL.Query().Get("after_cursor"), queryInt(r, "limit", 200))
	if err == nil {
		events = filterEventsForUser(events, act.user.ID)
		result["events"] = events
	}
	writeResult(w, result, err)
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	bearerProtocol := websocketBearerProtocol(r)
	if r.Header.Get("Authorization") == "" {
		if bearerProtocol != "" {
			r.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(bearerProtocol, websocketBearerProtocolPrefix))
		}
	}
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("realtime:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace_id is required"))
		return
	}
	if err := act.requireWorkspace(workspaceID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.store.GetWorkspace(r.Context(), workspaceID, act.user.ID); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	subscription, unsubscribe := s.hub.Subscribe(workspaceID)
	defer unsubscribe()
	acceptOptions := &websocket.AcceptOptions{OriginPatterns: s.websocketOriginPatterns(r)}
	if bearerProtocol != "" {
		acceptOptions.Subprotocols = []string{bearerProtocol}
	}
	conn, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := conn.CloseRead(r.Context())
	replayCursor := r.URL.Query().Get("after_cursor")
	// Revalidate the exact credential in the shared store: setup replay can replace
	// a bot secret without changing its token ID, including on another replica.
	bearerToken := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	credentialAuthorityLive := func() bool {
		var err error
		revokedReason := realtimeSessionRevokedCloseReason
		if act.botTokenID != "" {
			_, err = s.store.GetBotTokenAuth(ctx, bearerToken)
			revokedReason = "bot token revoked; reconnect with a valid token"
		} else if act.sessionToken != "" {
			_, err = s.sessionUser(ctx, act.sessionToken)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrSessionExpired) {
				_ = conn.Close(websocket.StatusPolicyViolation, revokedReason)
			} else {
				_ = conn.Close(websocket.StatusTryAgainLater, "credential verification unavailable; retry")
			}
			return false
		}
		return true
	}
	writeEvent := func(event store.Event) bool {
		return credentialAuthorityLive() && writeWS(ctx, conn, event) == nil
	}
	sessionRecheck := time.NewTicker(s.realtimeSessionCheck)
	defer sessionRecheck.Stop()
	// Startup and live delivery share one ordered, authorized durable-log drain.
	// Capture a finite tail each time; a wake received during this drain stays queued.
	drain := func() bool {
		if !credentialAuthorityLive() {
			return false
		}
		replayTail, err := s.store.LatestEventCursor(ctx, workspaceID, act.user.ID)
		if err != nil {
			_ = conn.Close(websocket.StatusTryAgainLater, realtimeReplayCloseReason)
			return false
		}
		if replayCursor != "" {
			exists, err := s.store.EventCursorExists(ctx, workspaceID, act.user.ID, replayCursor)
			if err != nil {
				_ = conn.Close(websocket.StatusTryAgainLater, realtimeReplayCloseReason)
				return false
			}
			if !exists {
				_ = conn.Close(realtimeResyncRequiredStatus, realtimeResyncRequiredCloseReason)
				return false
			}
		}
		if replayCursor != "" && (replayTail == "" || replayCursor > replayTail) {
			_ = conn.Close(realtimeResyncRequiredStatus, realtimeResyncRequiredCloseReason)
			return false
		}
		replayedEvents := 0
		for replayTail != "" && replayCursor < replayTail {
			pageCursor := replayCursor
			backlog, err := s.store.ListEventsAfter(ctx, workspaceID, act.user.ID, pageCursor, realtimeReplayPageSize)
			if err != nil {
				_ = conn.Close(websocket.StatusTryAgainLater, realtimeReplayCloseReason)
				return false
			}
			if len(backlog) == 0 {
				_ = conn.Close(realtimeResyncRequiredStatus, realtimeResyncRequiredCloseReason)
				return false
			}
			if pageCursor != "" {
				exists, err := s.store.EventCursorExists(ctx, workspaceID, act.user.ID, pageCursor)
				if err != nil {
					_ = conn.Close(websocket.StatusTryAgainLater, realtimeReplayCloseReason)
					return false
				}
				if !exists {
					_ = conn.Close(realtimeResyncRequiredStatus, realtimeResyncRequiredCloseReason)
					return false
				}
			}
			previousCursor := pageCursor
			for _, event := range backlog {
				if event.Cursor > replayTail {
					break
				}
				if replayedEvents >= s.realtimeReplayLimit {
					_ = conn.Close(realtimeResyncRequiredStatus, realtimeResyncRequiredCloseReason)
					return false
				}
				replayedEvents++
				// ListEventsAfter prefilters visibility, while this live lookup closes
				// the revocation window between fetching a page and writing its events.
				deliver, err := s.shouldDeliverEventToActorResult(ctx, event, act.user.ID)
				if err != nil {
					_ = conn.Close(websocket.StatusTryAgainLater, realtimeReplayCloseReason)
					return false
				}
				if !deliver {
					replayCursor = event.Cursor
					continue
				}
				if !writeEvent(event) {
					return false
				}
				replayCursor = event.Cursor
			}
			if replayCursor == previousCursor {
				_ = conn.Close(websocket.StatusTryAgainLater, realtimeReplayCloseReason)
				return false
			}
		}
		return true
	}
	if !drain() {
		return
	}
	for {
		// Prefer overflow termination to starting another durable drain.
		select {
		case <-subscription.Done:
			_ = conn.Close(websocket.StatusTryAgainLater, realtimeOverflowCloseReason)
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-subscription.Done:
			_ = conn.Close(websocket.StatusTryAgainLater, realtimeOverflowCloseReason)
			return
		case <-sessionRecheck.C:
			// An idle socket delivers nothing to revalidate against, so revocation
			// reaches it here instead of waiting for the workspace's next event.
			if !credentialAuthorityLive() {
				return
			}
		case _, ok := <-subscription.Wake:
			if !ok {
				continue
			}
			if !drain() {
				return
			}
		case event, ok := <-subscription.Events:
			if !ok {
				continue
			}
			// Revocations and other cursorless events are intentionally ephemeral.
			if eventRevokesWorkspaceAccess(event, act.user.ID) {
				_ = conn.Close(websocket.StatusPolicyViolation, "workspace access revoked")
				return
			}
			if !s.shouldDeliverEventToActor(ctx, event, act.user.ID) {
				continue
			}
			if !writeEvent(event) {
				return
			}
		}
	}
}

func websocketBearerProtocol(r *http.Request) string {
	for _, protocol := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		protocol = strings.TrimSpace(protocol)
		if strings.HasPrefix(protocol, websocketBearerProtocolPrefix) {
			return protocol
		}
	}
	return ""
}

func (s *Server) websocketOriginPatterns(r *http.Request) []string {
	publicURL, err := url.Parse(strings.TrimSpace(firstNonEmpty(s.frontendURL, s.githubOAuth.PublicURL)))
	if err != nil || publicURL.Host == "" {
		return nil
	}
	return []string{publicURL.Scheme + "://" + publicURL.Host}
}

// shouldDeliverEvent gates per-user-private events so they only reach allowed
// sessions and never leak to other workspace members.
func shouldDeliverEvent(event store.Event, userID string) bool {
	if len(event.RecipientUserIDs) > 0 {
		for _, allowed := range event.RecipientUserIDs {
			if allowed == userID {
				return true
			}
		}
		return false
	}
	switch event.Type {
	case "channel.read", "dm.read":
		payload, ok := event.Payload.(map[string]string)
		if !ok {
			// Backlog payloads come back via ListEventsAfter as map[string]any.
			if anyPayload, ok := event.Payload.(map[string]any); ok {
				if v, _ := anyPayload["user_id"].(string); v != "" {
					return v == userID
				}
				return false
			}
			return false
		}
		return payload["user_id"] == userID
	}
	return true
}

func eventRevokesWorkspaceAccess(event store.Event, userID string) bool {
	if event.Type != "bot.deleted" && event.Type != "bot.membership_removed" {
		return false
	}
	switch payload := event.Payload.(type) {
	case map[string]string:
		return payload["bot_user_id"] == userID
	case map[string]any:
		botUserID, _ := payload["bot_user_id"].(string)
		return botUserID == userID
	default:
		return false
	}
}

func filterEventsForUser(events []store.Event, userID string) []store.Event {
	filtered := events[:0]
	for _, event := range events {
		if shouldDeliverEvent(event, userID) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func (s *Server) shouldDeliverEventToActor(ctx context.Context, event store.Event, userID string) bool {
	deliver, err := s.shouldDeliverEventToActorResult(ctx, event, userID)
	return err == nil && deliver
}

func (s *Server) shouldDeliverEventToActorResult(ctx context.Context, event store.Event, userID string) (bool, error) {
	if !shouldDeliverEvent(event, userID) {
		return false, nil
	}
	var err error
	if conversationID := directConversationIDFromEvent(event); conversationID != "" {
		_, err = s.store.GetDirectConversation(ctx, conversationID, userID)
	} else if event.ChannelID == "" {
		_, err = s.store.GetWorkspace(ctx, event.WorkspaceID, userID)
	} else {
		_, err = s.store.GetChannel(ctx, event.ChannelID, userID)
	}
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func directConversationIDFromEvent(event store.Event) string {
	switch payload := event.Payload.(type) {
	case map[string]string:
		return payload["direct_conversation_id"]
	case map[string]any:
		conversationID, _ := payload["direct_conversation_id"].(string)
		return conversationID
	default:
		return ""
	}
}

func writeWS(ctx context.Context, conn *websocket.Conn, event store.Event) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, body)
}
