package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pushoverMessagesURL = "https://api.pushover.net/1/messages.json"

type PushoverNotifier struct {
	Token  string
	Client *http.Client
}

func NewPushoverNotifier(token string) *PushoverNotifier {
	return &PushoverNotifier{
		Token:  strings.TrimSpace(token),
		Client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *PushoverNotifier) Notify(ctx context.Context, notification PushNotification) error {
	if p == nil || p.Token == "" {
		return errors.New("pushover token is not configured")
	}
	user := strings.TrimSpace(notification.RecipientKey)
	if user == "" {
		return errors.New("pushover user key is required")
	}
	form := url.Values{}
	form.Set("token", p.Token)
	form.Set("user", user)
	form.Set("title", notification.Title)
	form.Set("message", notification.Message)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pushoverMessagesURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode >= 300 || result.Status != 1 {
		if len(result.Errors) > 0 {
			return fmt.Errorf("pushover rejected notification: %s", strings.Join(result.Errors, "; "))
		}
		return fmt.Errorf("pushover rejected notification with status %d", resp.StatusCode)
	}
	return nil
}
