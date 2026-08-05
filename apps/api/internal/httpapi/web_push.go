package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

var errWebPushSubscriptionGone = errors.New("web push subscription is no longer valid")

type WebPushService struct {
	subscriber      string
	vapidPublicKey  string
	vapidPrivateKey string
}

func newWebPushService(subscriber, publicKey, privateKey string) *WebPushService {
	publicKey = strings.TrimSpace(publicKey)
	privateKey = strings.TrimSpace(privateKey)
	if publicKey == "" || privateKey == "" {
		return nil
	}
	return &WebPushService{
		subscriber:      strings.TrimSpace(subscriber),
		vapidPublicKey:  publicKey,
		vapidPrivateKey: privateKey,
	}
}

func (w *WebPushService) PublicKey() string {
	if w == nil {
		return ""
	}
	return w.vapidPublicKey
}

type WebPushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

func (w *WebPushService) Notify(ctx context.Context, subscription store.WebPushSubscription, message WebPushMessage) error {
	if w == nil {
		return nil
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			Auth:   subscription.Keys.Auth,
			P256dh: subscription.Keys.P256DH,
		},
	}, &webpush.Options{
		Subscriber:      w.subscriber,
		VAPIDPublicKey:  w.vapidPublicKey,
		VAPIDPrivateKey: w.vapidPrivateKey,
		TTL:             30,
	})
	if resp != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			return errWebPushSubscriptionGone
		}
	}
	return err
}

func (s *Server) getWebPushPublicKey(w http.ResponseWriter, r *http.Request) {
	if s.webPush == nil || s.webPush.PublicKey() == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("web push is not configured"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_key": s.webPush.PublicKey()})
}

func (s *Server) upsertWebPushSubscription(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if s.webPush == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("web push is not configured"))
		return
	}
	var input store.WebPushSubscription
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	subscription, err := s.store.UpsertWebPushSubscription(r.Context(), store.UpsertWebPushSubscriptionInput{
		UserID:       act.user.ID,
		Subscription: input,
	})
	writeResult(w, map[string]any{"subscription": subscription}, err)
}

func (s *Server) deleteWebPushSubscription(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var input store.WebPushSubscription
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.DeleteWebPushSubscription(r.Context(), act.user.ID, input.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sendWebPushNotifications(ctx context.Context, recipients []store.PushNotificationRecipient, message WebPushMessage) {
	if s.webPush == nil {
		return
	}
	for _, recipient := range recipients {
		subscriptions, err := s.store.ListWebPushSubscriptions(ctx, recipient.UserID)
		if err != nil {
			continue
		}
		for _, subscription := range subscriptions {
			sendErr := s.webPush.Notify(ctx, subscription, message)
			if sendErr != nil && !errors.Is(sendErr, errWebPushSubscriptionGone) {
				continue
			}
			if errors.Is(sendErr, errWebPushSubscriptionGone) {
				_ = s.store.DeleteWebPushSubscription(ctx, recipient.UserID, subscription.Endpoint)
			}
		}
	}
}

func webPushMessageForNotification(message store.Message) WebPushMessage {
	return WebPushMessage{
		Title: notificationTitle(message),
		Body:  notificationBody(message),
		URL:   "/",
	}
}

func (s *Server) sendWebPushForMessage(ctx context.Context, message store.Message, recipients []store.PushNotificationRecipient) {
	s.sendWebPushNotifications(ctx, recipients, webPushMessageForNotification(message))
}
