package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

var errAmbiguousCookie = errors.New("multiple cookies with the same name are not allowed")

type actor struct {
	user store.User
	// sessionToken is the session this caller authenticated with, empty for
	// every other way of resolving an actor: bot tokens, a trusted-proxy
	// assertion, and the local development fallbacks. Handlers that revoke or
	// revalidate the caller's own session key on it.
	sessionToken string
	botTokenID   string
	workspaceID  string
	scopes       []string
}

var errSessionLookupUnavailable = errors.New("session verification unavailable; retry later")

// Only missing or expired sessions invalidate authentication. Backend failures
// must remain retryable across HTTP and already-open realtime connections.
func (s *Server) sessionUser(ctx context.Context, token string) (store.User, error) {
	user, err := s.store.GetSessionUser(ctx, token)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, store.ErrSessionExpired) {
		return store.User{}, fmt.Errorf("%w: %w", errSessionLookupUnavailable, err)
	}
	return user, err
}

func (s *Server) currentActor(r *http.Request) (actor, error) {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if botAuth, err := s.store.GetBotTokenAuth(r.Context(), token); err == nil {
			// Record ingress use once; realtime revalidation stays read-only.
			_ = s.store.RecordBotTokenUse(r.Context(), botAuth.TokenID)
			return actor{
				user:        botAuth.User,
				botTokenID:  botAuth.TokenID,
				workspaceID: botAuth.WorkspaceID,
				scopes:      botAuth.Scopes,
			}, nil
		}
		user, err := s.sessionUser(r.Context(), token)
		if err != nil {
			return actor{}, err
		}
		return actor{user: user, sessionToken: token}, nil
	}
	cookie, err := requestCookie(r, s.cookies.Session)
	if errors.Is(err, errAmbiguousCookie) {
		return actor{}, err
	}
	if err == nil && cookie.Value != "" {
		user, err := s.sessionUser(r.Context(), cookie.Value)
		if err == nil {
			return actor{user: user, sessionToken: cookie.Value}, nil
		}
		if errors.Is(err, errSessionLookupUnavailable) || s.access == nil || r.Header.Get(accessAssertionHeader) == "" {
			return actor{}, err
		}
	}
	if s.access != nil {
		if assertion := r.Header.Get(accessAssertionHeader); assertion != "" {
			if act, err := s.accessActor(r, assertion); err == nil {
				return act, nil
			}
		}
	}
	if s.disableDevAuth {
		return actor{}, errors.New("authentication required")
	}
	if !isLocalDevRequest(r) {
		return actor{}, errors.New("authentication required")
	}
	if id := r.Header.Get("X-ClickClack-User"); id != "" {
		user, err := s.store.GetUser(r.Context(), id)
		if err == nil && user.DeletedAt != nil {
			return actor{}, errors.New("authentication required")
		}
		return actor{user: user}, err
	}
	user, err := s.store.FirstUser(r.Context())
	return actor{user: user}, err
}

func (a actor) requireScope(scope string) error {
	if a.botTokenID == "" {
		return nil
	}
	for _, candidate := range a.scopes {
		if candidate == scope {
			return nil
		}
	}
	return errors.New("bot token is missing scope " + scope)
}

func (a actor) requireWorkspace(workspaceID string) error {
	if a.botTokenID == "" {
		return nil
	}
	if a.workspaceID == workspaceID {
		return nil
	}
	return errors.New("bot token cannot access this workspace")
}
