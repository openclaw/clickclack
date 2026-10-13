package httpapi

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/webpush"
)

// webPushEnabled reports whether the server can deliver web push. Without a
// configured key pair the endpoints answer as if the feature does not exist,
// and no subscription row is ever written.
func (s *Server) webPushEnabled() bool {
	return s.webPushNotifier != nil && s.webPushPublicKey != ""
}

func (s *Server) getMyPush(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("profile:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if !s.webPushEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":           false,
			"vapid_public_key":  "",
			"subscriptions":     []store.PushSubscription{},
			"this_device":       false,
			"this_device_stale": false,
		})
		return
	}
	subscriptions, err := s.store.ListPushSubscriptions(r.Context(), act.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	device, listed := pushDeviceListed(subscriptions, r.URL.Query().Get("device"))
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":           true,
		"vapid_public_key":  s.webPushPublicKey,
		"subscriptions":     subscriptions,
		"this_device":       listed,
		"this_device_stale": listed && store.PushKeyRetired(device.KeyID, s.webPushKeyID),
	})
}

// pushDeviceListed finds the browser asking, named by the digest of the
// subscription it holds, among these devices. One subscription serves every
// account on a browser, so this, not the account's device count, is what
// decides whether this device rings for this account.
func pushDeviceListed(subscriptions []store.PushSubscription, deviceKey string) (store.PushSubscription, bool) {
	if deviceKey == "" {
		return store.PushSubscription{}, false
	}
	for _, subscription := range subscriptions {
		if subscription.EndpointKey == deviceKey {
			return subscription, true
		}
	}
	return store.PushSubscription{}, false
}

var (
	errPushRegistrationUserRequired   = errors.New("user_id is required")
	errPushRegistrationAccountChanged = errors.New("this browser is now signed in to a different account; nothing was saved")
	errPushRemovalAccountChanged      = errors.New("this browser is now signed in to a different account; nothing was removed")
)

func (s *Server) putMyPushSubscription(w http.ResponseWriter, r *http.Request) {
	act, ok := s.requirePushActor(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		UserAgent string `json:"user_agent"`
		UserID    string `json:"user_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// One cookie jar serves every tab, so the account signed in now may not be
	// the one whose opt-in the client checked. The client names that account,
	// and a different one gets nothing written.
	if strings.TrimSpace(body.UserID) == "" {
		writeError(w, http.StatusBadRequest, errPushRegistrationUserRequired)
		return
	}
	if body.UserID != act.user.ID {
		writeError(w, http.StatusConflict, errPushRegistrationAccountChanged)
		return
	}
	if err := validatePushEndpoint(body.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := webpush.ValidateSubscriptionKeys(body.Keys.P256dh, body.Keys.Auth); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	subscription, err := s.store.UpsertPushSubscription(r.Context(), store.PushSubscriptionInput{
		UserID:           act.user.ID,
		Endpoint:         body.Endpoint,
		P256dh:           body.Keys.P256dh,
		Auth:             body.Keys.Auth,
		UserAgent:        body.UserAgent,
		SessionToken:     pushSessionToken(act),
		DevelopmentActor: act.developmentFallback,
		KeyID:            s.webPushKeyID,
	})
	if err == nil {
		log.Printf("web push device %s for user %s via %s", pushRegistrationKind(subscription), act.user.ID, webpush.RelayHost(body.Endpoint))
	}
	writeResult(w, map[string]any{"subscription": subscription}, err)
}

func (s *Server) deleteMyPushSubscription(w http.ResponseWriter, r *http.Request) {
	act, ok := s.requirePushActor(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		UserID   string `json:"user_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// The removal is scoped to the signed-in account, so a tab still showing
	// another one would remove nothing of that account's while it believed the
	// device was off. The client names the account it is turning off for.
	if strings.TrimSpace(body.UserID) == "" {
		writeError(w, http.StatusBadRequest, errPushRegistrationUserRequired)
		return
	}
	if body.UserID != act.user.ID {
		writeError(w, http.StatusConflict, errPushRemovalAccountChanged)
		return
	}
	endpoint := strings.TrimSpace(body.Endpoint)
	if endpoint == "" {
		writeError(w, http.StatusBadRequest, errors.New("endpoint is required"))
		return
	}
	if err := s.store.DeletePushSubscription(r.Context(), act.user.ID, endpoint); err != nil {
		writeStoreError(w, err)
		return
	}
	log.Printf("web push device removed by user %s", act.user.ID)
	w.WriteHeader(http.StatusNoContent)
}

// requirePushActor resolves the signed-in human behind a subscription change.
// Bot tokens have no device to notify, and a server without keys has no
// subscriptions at all.
func (s *Server) requirePushActor(w http.ResponseWriter, r *http.Request) (actor, bool) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return actor{}, false
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("bot tokens cannot register push subscriptions"))
		return actor{}, false
	}
	if !s.webPushEnabled() {
		writeError(w, http.StatusNotFound, errors.New("web push is not configured"))
		return actor{}, false
	}
	// A device follows the session that registered it. Only the development
	// fallback is signed in with no session; any other sessionless caller
	// would leave a row that signing out could never stop.
	if !pushRegistrationAllowed(act) {
		writeError(w, http.StatusForbidden, store.ErrPushSubscriptionNeedsSession)
		return actor{}, false
	}
	return act, true
}

// pushRegistrationAllowed is true for a caller with a session to bind the
// device to, and for the development fallback, the one identity with none.
func pushRegistrationAllowed(act actor) bool {
	return pushSessionToken(act) != "" || act.developmentFallback
}

// pushSessionToken is the session a registration binds to: the one the caller
// presented, or the one a trusted-proxy assertion minted for this request.
func pushSessionToken(act actor) string {
	if act.sessionToken != "" {
		return act.sessionToken
	}
	return act.accessSessionToken
}

// pushRegistrationKind names a registration for the log. A first write has
// matching timestamps; a later one for the same endpoint moves only one.
func pushRegistrationKind(subscription store.PushSubscription) string {
	if subscription.CreatedAt == subscription.UpdatedAt {
		return "registered"
	}
	return "refreshed"
}

func validatePushEndpoint(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return errors.New("endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return errors.New("endpoint must be an absolute push service URL")
	}
	return validatePushEndpointHost(parsed.Scheme, parsed.Hostname())
}
