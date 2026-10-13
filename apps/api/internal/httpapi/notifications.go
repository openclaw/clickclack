package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// PushNotification is one alert for one recipient. Pushover reads the
// recipient key, the title, and the message. The web push notifier reads the
// title and the rest and ignores the key; it is handed no message, because it
// reads the text from the stored message when the push is sent.
type PushNotification struct {
	RecipientKey  string
	Title         string
	Message       string
	UserID        string
	MessageID     string
	Tag           string
	URL           string
	Subscriptions []store.PushSubscriptionTarget
}

type PushNotifier interface {
	Notify(ctx context.Context, notification PushNotification) error
}

func (s *Server) notifyMessageCreated(ctx context.Context, message store.Message, mentionedUserIDs []string) {
	if s.pushNotifier == nil && s.webPushNotifier == nil {
		return
	}
	recipients, err := s.store.ListPushNotificationRecipients(ctx, message.ID, mentionedUserIDs)
	if err != nil {
		log.Printf("push notification recipient lookup failed: %v", err)
		return
	}
	place := s.notificationPlace(ctx, message)
	for _, recipient := range recipients {
		if !s.canNotifyMessageRecipient(ctx, message, recipient.UserID) {
			continue
		}
		if s.pushNotifier != nil && recipient.PushoverUserKey != "" {
			notification := PushNotification{
				RecipientKey: recipient.PushoverUserKey,
				Title:        notificationTitle(message),
				Message:      notificationBody(message),
			}
			if err := s.pushNotifier.Notify(ctx, notification); err != nil {
				log.Printf("push notification failed for user %s: %v", recipient.UserID, err)
			}
		}
		if s.webPushNotifier == nil {
			continue
		}
		subscriptions := currentKeySubscriptions(recipient.Subscriptions, s.webPushKeyID)
		if len(subscriptions) == 0 {
			continue
		}
		notification := PushNotification{
			UserID:        recipient.UserID,
			MessageID:     message.ID,
			Title:         webPushTitle(message, place),
			Tag:           webPushTag(message),
			URL:           webPushURL(message),
			Subscriptions: subscriptions,
		}
		if err := s.webPushNotifier.Notify(ctx, notification); err != nil {
			log.Printf("web push notification failed for user %s: %v", recipient.UserID, err)
		}
	}
}

// currentKeySubscriptions drops the devices registered under a key the server
// no longer signs with. Their push service refuses every push, so a request
// would only spend a worker; opening the app replaces the subscription.
func currentKeySubscriptions(targets []store.PushSubscriptionTarget, keyID string) []store.PushSubscriptionTarget {
	current := make([]store.PushSubscriptionTarget, 0, len(targets))
	for _, target := range targets {
		if !store.PushKeyRetired(target.KeyID, keyID) {
			current = append(current, target)
		}
	}
	return current
}

// notificationPlace resolves the channel a message was posted in once per
// message. The author is a member wherever they can post, so their view is the
// cheapest one to ask with.
func (s *Server) notificationPlace(ctx context.Context, message store.Message) store.Channel {
	if s.webPushNotifier == nil || message.ChannelID == "" {
		return store.Channel{}
	}
	channel, err := s.store.GetChannel(ctx, message.ChannelID, message.AuthorID)
	if err != nil {
		return store.Channel{}
	}
	return channel
}

// webPushTitle matches the title the in-page notification uses, so a device
// that sees both paths reads one sentence, not two shapes of it. An author
// without a name reads as the product, as it does in the page, never as an
// internal id on a lock screen.
func webPushTitle(message store.Message, place store.Channel) string {
	author := "ClickClack"
	if message.Author != nil && strings.TrimSpace(message.Author.DisplayName) != "" {
		author = message.Author.DisplayName
	}
	if title := channelDisplayTitle(place); title != "" {
		return author + " in #" + title
	}
	if message.DirectConversationID != "" {
		return author + " in Direct message"
	}
	// A channel that could not be read names nobody rather than claiming to
	// be a conversation it is not.
	return author
}

// webPushBody is the text a push carries. The notifier builds it from the
// message as it reads at send time, so an edit made while the push waited is
// what the phone shows; the sender truncates it.
func webPushBody(message store.Message) string {
	body := strings.TrimSpace(message.Body)
	if body == "" {
		return "New message"
	}
	return body
}

// webPushTag matches the tag the in-page notification uses so a device showing
// both collapses them into one.
func webPushTag(message store.Message) string {
	return "clickclack:" + message.ID
}

// webPushURL routes a tap to the conversation or channel the message belongs
// to. A thread reply lands in its channel: message routes are minted on
// demand. The workspace segment is a storage id, and the route API resolves
// only storage targets beside one, so the target is always a storage id too
// (never a channel's route id) and the app canonicalizes the pair on arrival,
// as it does for an in-page notification with no route id to hand.
func webPushURL(message store.Message) string {
	target := message.ChannelID
	if message.DirectConversationID != "" {
		target = message.DirectConversationID
	}
	if message.WorkspaceID == "" || target == "" {
		return "/app"
	}
	return "/app/" + url.PathEscape(message.WorkspaceID) + "/" + url.PathEscape(target)
}

func channelDisplayTitle(channel store.Channel) string {
	if channel.DisplayTitle != nil && strings.TrimSpace(*channel.DisplayTitle) != "" {
		return strings.TrimSpace(*channel.DisplayTitle)
	}
	return strings.TrimSpace(channel.Name)
}

func messageEventMentionedUserIDs(events []store.Event) []string {
	for _, event := range events {
		if event.Type == "message.created" || event.Type == "thread.reply_created" {
			return event.MentionedUserIDs
		}
	}
	return nil
}

func (s *Server) canNotifyMessageRecipient(ctx context.Context, message store.Message, userID string) bool {
	_, err := s.store.GetMessage(ctx, message.ID, userID)
	return err == nil
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
