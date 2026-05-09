package httpapi

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type PushNotification struct {
	RecipientKey string
	Title        string
	Message      string
}

type PushNotifier interface {
	Notify(ctx context.Context, notification PushNotification) error
}

func (s *Server) notifyMessageCreated(ctx context.Context, message store.Message) {
	if s.pushNotifier == nil {
		return
	}
	recipients, err := s.store.ListPushNotificationRecipients(ctx, message.ID)
	if err != nil {
		log.Printf("push notification recipient lookup failed: %v", err)
		return
	}
	for _, recipient := range recipients {
		notification := PushNotification{
			RecipientKey: recipient.PushoverUserKey,
			Title:        notificationTitle(message),
			Message:      notificationBody(message),
		}
		if err := s.pushNotifier.Notify(ctx, notification); err != nil {
			log.Printf("push notification failed for user %s: %v", recipient.UserID, err)
		}
	}
}

func notificationTitle(message store.Message) string {
	if message.DirectConversationID != "" {
		return "ClickClack DM"
	}
	if message.ParentMessageID != nil {
		return "ClickClack thread"
	}
	return "ClickClack"
}

func notificationBody(message store.Message) string {
	author := message.AuthorID
	if message.Author != nil && strings.TrimSpace(message.Author.DisplayName) != "" {
		author = message.Author.DisplayName
	}
	body := strings.TrimSpace(message.Body)
	bodyRunes := []rune(body)
	if len(bodyRunes) > 500 {
		body = string(bodyRunes[:500]) + "..."
	}
	if body == "" {
		return fmt.Sprintf("%s sent a message", author)
	}
	return fmt.Sprintf("%s: %s", author, body)
}
