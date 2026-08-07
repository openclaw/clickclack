package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const cognitionRequestTimeout = 5 * time.Second

type cognitionAnalyzeRequest struct {
	Content string           `json:"content"`
	Context *cognitionContext `json:"context,omitempty"`
}

type cognitionContext struct {
	ChannelID   string `json:"channel_id,omitempty"`
	WorkspaceID string `json:"workspace_id"`
}

type cognitionAnalyzeResponse struct {
	Intent              string   `json:"intent,omitempty"`
	Persona             string   `json:"persona,omitempty"`
	Confidence          *float64 `json:"confidence,omitempty"`
	ContextTags         []string `json:"context_tags,omitempty"`
	ClarificationQuestion string  `json:"clarification_question,omitempty"`
}

// analyzeOnIngest fires a non-blocking goroutine that sends the message to the
// cognition service and updates metadata on success. It must never block the
// caller, never fail the send, and never panic.
func (s *Server) analyzeOnIngest(msg store.Message) {
	if s.cognitionURL == "" {
		return
	}
	if strings.TrimSpace(msg.Body) == "" {
		return
	}
	// Only analyze ordinary user/bot text messages, not agent activity rows.
	if store.IsActivityMessageKind(msg.Kind) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cognitionRequestTimeout)
		defer cancel()

		result, err := s.callCognition(ctx, msg.Body, msg.ChannelID, msg.WorkspaceID)
		if err != nil {
			log.Printf("cognition: analyze failed for message %s: %v", msg.ID, err)
			return
		}

		input := store.UpdateMessageMetadataInput{
			MessageID: msg.ID,
			UserID:    msg.AuthorID,
		}
		hasField := false
		if result.Intent != "" {
			input.Intent = &result.Intent
			hasField = true
		}
		if result.Persona != "" {
			input.Persona = &result.Persona
			hasField = true
		}
		if result.Confidence != nil {
			input.Confidence = result.Confidence
			hasField = true
		}
		if result.ContextTags != nil {
			raw, err := json.Marshal(result.ContextTags)
			if err == nil {
				ctxJSON := string(raw)
				input.ContextJSON = &ctxJSON
				hasField = true
			}
		}
		if !hasField {
			return
		}

		if _, err := s.store.UpdateMessageMetadata(ctx, input); err != nil {
			log.Printf("cognition: metadata update failed for message %s: %v", msg.ID, err)
		}
	}()
}

func (s *Server) callCognition(ctx context.Context, body, channelID, workspaceID string) (cognitionAnalyzeResponse, error) {
	reqBody := cognitionAnalyzeRequest{
		Content: body,
		Context: &cognitionContext{
			ChannelID:   channelID,
			WorkspaceID: workspaceID,
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return cognitionAnalyzeResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cognitionURL+"/analyze", bytes.NewReader(payload))
	if err != nil {
		return cognitionAnalyzeResponse{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cognitionToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cognitionToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cognitionAnalyzeResponse{}, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cognitionAnalyzeResponse{}, fmt.Errorf("cognition returned status %d", resp.StatusCode)
	}

	var result cognitionAnalyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return cognitionAnalyzeResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}
