package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("profile:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	preferences, err := s.store.GetAppearancePreferences(r.Context(), act.user.ID)
	payload := currentUserPayload{User: act.user, AppearancePreferences: preferences}
	if err == nil {
		payload.PasswordEnrolled, err = s.passwordEnrolled(r.Context(), act.user.ID)
	}
	writeResult(w, map[string]any{"user": payload}, err)
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot update profiles"))
		return
	}
	var body struct {
		DisplayName           *string                           `json:"display_name"`
		Handle                *string                           `json:"handle"`
		AvatarURL             *string                           `json:"avatar_url"`
		NotificationSettings  *store.NotificationSettings       `json:"notification_settings"`
		AppearancePreferences *store.AppearancePreferencesPatch `json:"appearance_preferences"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := s.store.UpdateCurrentUser(r.Context(), store.UpdateCurrentUserInput{
		UserID:                act.user.ID,
		DisplayName:           body.DisplayName,
		Handle:                body.Handle,
		AvatarURL:             body.AvatarURL,
		NotificationSettings:  body.NotificationSettings,
		AppearancePreferences: body.AppearancePreferences,
	})
	payload := currentUserPayload{User: updated.User, AppearancePreferences: updated.AppearancePreferences}
	if err == nil {
		payload.PasswordEnrolled, err = s.passwordEnrolled(r.Context(), updated.User.ID)
	}
	writeResult(w, map[string]any{"user": payload}, err)
}

type currentUserPayload struct {
	store.User
	AppearancePreferences *store.AppearancePreferences `json:"appearance_preferences,omitempty"`
	PasswordEnrolled      bool                         `json:"password_enrolled"`
}

// passwordEnrolled reports whether an account has a password on file. The SPA
// pairs it with the advertised auth methods to decide whether to offer the
// change-password form. It is reported only for the caller's own account, so it
// discloses nothing about who else can sign in with a password.
func (s *Server) passwordEnrolled(ctx context.Context, userID string) (bool, error) {
	hash, err := s.store.GetUserPasswordHash(ctx, userID)
	return hash != "", err
}
