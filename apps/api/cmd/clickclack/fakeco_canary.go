package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type canaryOptions struct {
	CorrelationID    string
	RunID            string
	GatewayHealthURL string
	Timeout          time.Duration
	PollInterval     time.Duration
}

type canaryResult struct {
	Status              string `json:"status"`
	CorrelationID       string `json:"correlation_id"`
	RunID               string `json:"run_id,omitempty"`
	CaseID              string `json:"case_id"`
	GatewayPreflight    bool   `json:"gateway_preflight"`
	WorkspaceID         string `json:"workspace_id"`
	ChannelID           string `json:"channel_id"`
	RequestMessageID    string `json:"request_message_id"`
	ResponseMessageID   string `json:"response_message_id"`
	ElapsedMilliseconds int64  `json:"elapsed_ms"`
}

func (c apiClient) canary(args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("canary", flag.ExitOnError)
	addClientFlags(flags, &opts)
	correlationID := flags.String("correlation-id", "", "optional safe correlation ID")
	runID := flags.String("run-id", "", "optional safe external run ID for evidence")
	gatewayHealthURL := flags.String("gateway-health-url", os.Getenv("OPENCLAW_GATEWAY_HEALTH_URL"), "optional OpenClaw health URL")
	timeout := flags.Duration("timeout", 2*time.Minute, "overall canary timeout")
	pollInterval := flags.Duration("poll-interval", time.Second, "reply polling interval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	result, err := c.runCanary(context.Background(), canaryOptions{
		CorrelationID:    *correlationID,
		RunID:            *runID,
		GatewayHealthURL: *gatewayHealthURL,
		Timeout:          *timeout,
		PollInterval:     *pollInterval,
	})
	if err != nil {
		return err
	}
	return c.write(result, result.CorrelationID, fmt.Sprintf("canary %s passed: request %s, response %s\n", result.CorrelationID, result.RequestMessageID, result.ResponseMessageID))
}

func (c apiClient) runCanary(parent context.Context, options canaryOptions) (canaryResult, error) {
	if options.Timeout <= 0 {
		return canaryResult{}, errors.New("canary timeout must be positive")
	}
	if options.PollInterval <= 0 {
		return canaryResult{}, errors.New("canary poll interval must be positive")
	}
	correlationID := strings.TrimSpace(options.CorrelationID)
	if correlationID == "" {
		correlationID = newCanaryCorrelationID()
	}
	if !validCanaryCorrelationID(correlationID) {
		return canaryResult{}, errors.New("correlation ID must be 1-80 letters, numbers, dots, underscores, colons, or hyphens")
	}
	runID := strings.TrimSpace(options.RunID)
	if runID != "" && !validCanaryCorrelationID(runID) {
		return canaryResult{}, errors.New("run ID must be at most 80 letters, numbers, dots, underscores, colons, or hyphens")
	}
	c.correlationID = correlationID
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	started := time.Now()
	if err := validateCanaryServerURL(c.opts.Server); err != nil {
		return canaryResult{}, err
	}

	gatewayPreflight := false
	if strings.TrimSpace(options.GatewayHealthURL) != "" {
		if err := c.probeGatewayHealth(ctx, options.GatewayHealthURL); err != nil {
			return canaryResult{}, err
		}
		gatewayPreflight = true
	}
	user, err := c.currentUserContext(ctx)
	if err != nil {
		return canaryResult{}, fmt.Errorf("resolve canary user: %w", err)
	}
	if user.Kind != "human" {
		return canaryResult{}, errors.New("canary requires a human session token because OpenClaw ignores bot-authored messages")
	}
	channel, err := c.resolveChannelContext(ctx)
	if err != nil {
		return canaryResult{}, fmt.Errorf("resolve canary channel: %w", err)
	}
	prompt := fmt.Sprintf("FakeCo canary %s. Reply with exactly: fakeco-canary-ok %s", correlationID, correlationID)
	var created struct {
		Message store.Message `json:"message"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/channels/"+url.PathEscape(channel.ID)+"/messages", map[string]string{
		"body":  prompt,
		"nonce": newCanaryNonce(correlationID),
	}, &created); err != nil {
		return canaryResult{}, fmt.Errorf("post canary message: %w", err)
	}
	if created.Message.ChannelSeq == nil {
		return canaryResult{}, errors.New("canary request did not receive a channel sequence")
	}

	cursor := *created.Message.ChannelSeq
	for {
		var page store.MessagePage
		path := "/api/channels/" + url.PathEscape(channel.ID) + "/messages?after_seq=" + strconv.FormatInt(cursor, 10) + "&limit=100"
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			if ctx.Err() != nil {
				return canaryResult{}, fmt.Errorf("canary %s timed out waiting for an OpenClaw reply", correlationID)
			}
			return canaryResult{}, fmt.Errorf("poll canary reply: %w", err)
		}
		for _, message := range page.Messages {
			if !isCanaryReply(message, created.Message.ID, correlationID) {
				continue
			}
			return canaryResult{
				Status:              "passed",
				CorrelationID:       correlationID,
				RunID:               runID,
				CaseID:              created.Message.ID,
				GatewayPreflight:    gatewayPreflight,
				WorkspaceID:         channel.WorkspaceID,
				ChannelID:           channel.ID,
				RequestMessageID:    created.Message.ID,
				ResponseMessageID:   message.ID,
				ElapsedMilliseconds: time.Since(started).Milliseconds(),
			}, nil
		}
		if page.HasNewer && page.NewestSeq <= cursor {
			return canaryResult{}, errors.New("canary message pagination did not advance")
		}
		if page.NewestSeq > cursor {
			cursor = page.NewestSeq
		}
		if page.HasNewer {
			continue
		}
		timer := time.NewTimer(options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return canaryResult{}, fmt.Errorf("canary %s timed out waiting for an OpenClaw reply", correlationID)
		case <-timer.C:
		}
	}
}

func (c apiClient) probeGatewayHealth(ctx context.Context, rawURL string) error {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return errors.New("gateway health URL must be an http(s) URL without embedded credentials, query, or fragment")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Correlation-ID", c.correlationID)
	client := *c.http
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("OpenClaw gateway health preflight: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OpenClaw gateway health preflight: %s", resp.Status)
	}
	return nil
}

func validateCanaryServerURL(rawURL string) error {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return errors.New("ClickClack server URL must be an http(s) base URL without embedded credentials, query, or fragment")
	}
	return nil
}

func isCanaryReply(message store.Message, requestMessageID, correlationID string) bool {
	if message.Author == nil || message.Author.Kind != "bot" {
		return false
	}
	if message.Kind != "" && message.Kind != store.MessageKindMessage {
		return false
	}
	if message.QuotedMessageID == nil || *message.QuotedMessageID != requestMessageID {
		return false
	}
	return strings.TrimSpace(message.Body) == "fakeco-canary-ok "+correlationID
}

func validCanaryCorrelationID(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func newCanaryCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "fakeco-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("fakeco-%d", time.Now().UnixNano())
}

func newCanaryNonce(correlationID string) string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "fakeco-canary." + correlationID + "." + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("fakeco-canary.%s.%d", correlationID, time.Now().UnixNano())
}
