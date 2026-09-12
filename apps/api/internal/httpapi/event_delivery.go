package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) publishEvent(ctx context.Context, event store.Event) {
	s.hub.Publish(event)
	if event.ID == "" || event.Cursor == "" {
		return
	}
	s.deliverEventSubscriptions(ctx, event)
}

func (s *Server) recordAudit(ctx context.Context, workspaceID, actorUserID, action, targetType, targetID string, metadata map[string]any) {
	_, _ = s.store.CreateAuditLogEntry(ctx, store.CreateAuditLogEntryInput{
		WorkspaceID: workspaceID,
		ActorUserID: actorUserID,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		Metadata:    metadata,
	})
}

func (s *Server) publishEvents(ctx context.Context, events []store.Event) {
	for _, event := range events {
		s.publishEvent(ctx, event)
	}
}

func (s *Server) deliverEventSubscriptions(ctx context.Context, event store.Event) {
	subscriptions, err := s.store.ListEventSubscriptionsForEvent(ctx, event)
	if err != nil {
		return
	}
	for _, subscription := range subscriptions {
		payload, err := json.Marshal(map[string]any{
			"subscription_id": subscription.ID,
			"event":           event,
		})
		if err != nil {
			continue
		}
		status, responseBody, deliveryErr := s.postEventCallback(ctx, subscription, event, payload)
		errorText := ""
		if deliveryErr != nil {
			errorText = deliveryErr.Error()
		}
		_, _ = s.store.CreateEventDeliveryAttempt(ctx, store.CreateEventDeliveryAttemptInput{
			SubscriptionID: subscription.ID,
			EventID:        event.ID,
			WorkspaceID:    event.WorkspaceID,
			EventType:      event.Type,
			RequestJSON:    string(payload),
			ResponseStatus: status,
			ResponseBody:   responseBody,
			Error:          errorText,
		})
	}
}

func (s *Server) postEventCallback(ctx context.Context, subscription store.EventSubscription, event store.Event, payload []byte) (int, string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.CallbackURL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-Timestamp", timestamp)
	req.Header.Set("X-ClickClack-Event-ID", event.ID)
	req.Header.Set("X-ClickClack-Signature", signSlashCallback(subscription.SigningSecret, timestamp, payload))
	resp, err := s.callbackClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, string(body), errors.New("event subscription callback failed")
	}
	return resp.StatusCode, string(body), nil
}

func (s *Server) postSlashCallback(ctx context.Context, command store.SlashCommand, payload []byte) (int, string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, command.CallbackURL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-Timestamp", timestamp)
	req.Header.Set("X-ClickClack-Signature", signSlashCallback(command.SigningSecret, timestamp, payload))
	resp, err := s.callbackClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, string(body), errors.New("slash command callback failed")
	}
	return resp.StatusCode, string(body), nil
}

func signSlashCallback(secret, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
