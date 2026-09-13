package store

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Question lifecycle statuses. A bot posts an open question; the first valid
// answer moves it to submitted; the authoring bot records the outcome.
const (
	QuestionStatusOpen      = "open"
	QuestionStatusSubmitted = "submitted"
	QuestionStatusAnswered  = "answered"
	QuestionStatusCancelled = "cancelled"
	QuestionStatusExpired   = "expired"
	QuestionStatusFailed    = "failed"
)

const (
	QuestionResponseSourceClickClack = "clickclack"
	QuestionResponseSourceExternal   = "external"
)

const (
	MaxQuestionItems              = 5
	MaxQuestionOptions            = 10
	MaxQuestionResponders         = 50
	MaxQuestionTitleLength        = 300
	MaxQuestionHeaderLength       = 24
	MaxQuestionPromptLength       = 1000
	MaxQuestionOptionLabelLength  = 80
	MaxQuestionOptionDescription  = 200
	MaxQuestionPlaceholderLength  = 60
	MaxQuestionFreeTextLength     = 2000
	MaxQuestionNoteLength         = 200
	MaxQuestionExternalIDLength   = 128
	MaxQuestionURLLength          = 2048
	MaxBotQuestionPageSize        = 200
	MinQuestionLifetime           = 10 * time.Second
	MaxQuestionLifetime           = 7 * 24 * time.Hour
	QuestionCapabilityHeader      = "X-ClickClack-Questions"
	QuestionCapabilityHeaderValue = "supported"
)

var (
	// ErrInvalidQuestion wraps every question or answer validation failure.
	ErrInvalidQuestion = errors.New("invalid question")
	// ErrQuestionClosed is returned when a question no longer accepts answers.
	ErrQuestionClosed = errors.New("question is no longer open")
	// ErrQuestionResponderRequired is returned to people outside the responder list.
	ErrQuestionResponderRequired = errors.New("only the listed responders can answer this question")
	// ErrQuestionAuthorRequired is returned when a caller other than the authoring bot resolves a question.
	ErrQuestionAuthorRequired = errors.New("only the bot that asked this question can resolve it")
	// ErrQuestionConflict is returned for stale versions and transitions the current status does not allow.
	ErrQuestionConflict = errors.New("question changed; reload it and try again")
)

var questionItemIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionItem struct {
	ID               string           `json:"id"`
	Header           string           `json:"header"`
	Prompt           string           `json:"prompt"`
	URL              string           `json:"url,omitempty"`
	Options          []QuestionOption `json:"options,omitempty"`
	MultiSelect      bool             `json:"multi_select,omitempty"`
	AllowOther       bool             `json:"allow_other,omitempty"`
	OtherPlaceholder string           `json:"other_placeholder,omitempty"`
}

// QuestionSpec is the question a bot attaches when it creates a message.
type QuestionSpec struct {
	ExternalID       string         `json:"external_id,omitempty"`
	Title            string         `json:"title,omitempty"`
	ExpiresAt        string         `json:"expires_at"`
	ResponderUserIDs []string       `json:"responder_user_ids,omitempty"`
	AllowSkip        *bool          `json:"allow_skip,omitempty"`
	Items            []QuestionItem `json:"items"`
}

type QuestionResponse struct {
	Answers     map[string][]string `json:"answers,omitempty"`
	Skipped     bool                `json:"skipped,omitempty"`
	Source      string              `json:"source"`
	Responder   *User               `json:"responder,omitempty"`
	RespondedAt string              `json:"responded_at,omitempty"`
}

// MessageQuestion is the question facet hydrated on a message.
type MessageQuestion struct {
	Status           string            `json:"status"`
	ExternalID       string            `json:"external_id,omitempty"`
	Title            string            `json:"title,omitempty"`
	ExpiresAt        string            `json:"expires_at"`
	AllowSkip        bool              `json:"allow_skip"`
	Items            []QuestionItem    `json:"items"`
	ResponderUserIDs []string          `json:"responder_user_ids,omitempty"`
	Response         *QuestionResponse `json:"response,omitempty"`
	Note             string            `json:"note,omitempty"`
	ResolvedAt       *string           `json:"resolved_at,omitempty"`
	Version          int64             `json:"version"`
}

type AnswerQuestionInput struct {
	MessageID string
	UserID    string
	Answers   map[string][]string
	Skip      bool
	Nonce     string
	// ExpectedVersion is the question version the person saw. A different
	// current version, such as after a reopen, rejects the answer.
	ExpectedVersion *int64
}

type ResolveQuestionInput struct {
	MessageID       string
	BotUserID       string
	Status          string
	Note            string
	Answers         map[string][]string
	ExpectedVersion *int64
}

// BotQuestion summarizes one of a bot's questions for reconciliation.
type BotQuestion struct {
	MessageID            string `json:"message_id"`
	WorkspaceID          string `json:"workspace_id"`
	ChannelID            string `json:"channel_id,omitempty"`
	DirectConversationID string `json:"direct_conversation_id,omitempty"`
	ThreadRootID         string `json:"thread_root_id"`
	ExternalID           string `json:"external_id,omitempty"`
	Status               string `json:"status"`
	ExpiresAt            string `json:"expires_at"`
	Version              int64  `json:"version"`
}

func invalidQuestion(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuestion, fmt.Sprintf(format, args...))
}

// ValidateQuestionLifetime checks the deadline of a question that is about to
// be created. Replaying an existing question skips it, so a retry near the
// deadline still returns the original message.
func ValidateQuestionLifetime(spec QuestionSpec, now time.Time) error {
	expiresAt, err := time.Parse(time.RFC3339Nano, spec.ExpiresAt)
	if err != nil {
		return invalidQuestion("expires_at must be an RFC 3339 timestamp")
	}
	if lifetime := expiresAt.Sub(now); lifetime < MinQuestionLifetime || lifetime > MaxQuestionLifetime {
		return invalidQuestion("expires_at must be between %s and %s from now", MinQuestionLifetime, MaxQuestionLifetime)
	}
	return nil
}

// NormalizeQuestionSpec trims and validates the shape of a bot-authored
// question. New questions also need ValidateQuestionLifetime.
func NormalizeQuestionSpec(input QuestionSpec) (QuestionSpec, error) {
	spec := QuestionSpec{
		ExternalID: strings.TrimSpace(input.ExternalID),
		Title:      strings.TrimSpace(input.Title),
		ExpiresAt:  strings.TrimSpace(input.ExpiresAt),
	}
	if utf8.RuneCountInString(spec.ExternalID) > MaxQuestionExternalIDLength {
		return QuestionSpec{}, invalidQuestion("external_id is longer than %d characters", MaxQuestionExternalIDLength)
	}
	if utf8.RuneCountInString(spec.Title) > MaxQuestionTitleLength {
		return QuestionSpec{}, invalidQuestion("title is longer than %d characters", MaxQuestionTitleLength)
	}
	expiresAt, err := time.Parse(time.RFC3339, spec.ExpiresAt)
	if err != nil {
		return QuestionSpec{}, invalidQuestion("expires_at must be an RFC 3339 timestamp")
	}
	spec.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	allowSkip := input.AllowSkip == nil || *input.AllowSkip
	spec.AllowSkip = &allowSkip
	for _, id := range input.ResponderUserIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return QuestionSpec{}, invalidQuestion("responder_user_ids cannot contain empty IDs")
		}
		if !slices.Contains(spec.ResponderUserIDs, id) {
			spec.ResponderUserIDs = append(spec.ResponderUserIDs, id)
		}
	}
	if len(spec.ResponderUserIDs) > MaxQuestionResponders {
		return QuestionSpec{}, invalidQuestion("responder_user_ids has more than %d users", MaxQuestionResponders)
	}
	if len(input.Items) == 0 || len(input.Items) > MaxQuestionItems {
		return QuestionSpec{}, invalidQuestion("items must contain 1 to %d questions", MaxQuestionItems)
	}
	seenIDs := map[string]bool{}
	for index, raw := range input.Items {
		item, err := normalizeQuestionItem(raw)
		if err != nil {
			return QuestionSpec{}, fmt.Errorf("%w (items[%d])", err, index)
		}
		if seenIDs[item.ID] {
			return QuestionSpec{}, invalidQuestion("items[%d].id %q is repeated", index, item.ID)
		}
		seenIDs[item.ID] = true
		spec.Items = append(spec.Items, item)
	}
	return spec, nil
}

func normalizeQuestionItem(raw QuestionItem) (QuestionItem, error) {
	item := QuestionItem{
		ID:               strings.TrimSpace(raw.ID),
		Header:           strings.TrimSpace(raw.Header),
		Prompt:           strings.TrimSpace(raw.Prompt),
		URL:              strings.TrimSpace(raw.URL),
		MultiSelect:      raw.MultiSelect,
		AllowOther:       raw.AllowOther,
		OtherPlaceholder: strings.TrimSpace(raw.OtherPlaceholder),
	}
	if !questionItemIDPattern.MatchString(item.ID) {
		return QuestionItem{}, invalidQuestion("id must match %s", questionItemIDPattern)
	}
	if item.Header == "" || utf8.RuneCountInString(item.Header) > MaxQuestionHeaderLength {
		return QuestionItem{}, invalidQuestion("header must have 1 to %d characters", MaxQuestionHeaderLength)
	}
	if item.Prompt == "" || utf8.RuneCountInString(item.Prompt) > MaxQuestionPromptLength {
		return QuestionItem{}, invalidQuestion("prompt must have 1 to %d characters", MaxQuestionPromptLength)
	}
	if item.URL != "" && !safeQuestionURL(item.URL) {
		return QuestionItem{}, invalidQuestion("url must be an http or https URL of at most %d characters", MaxQuestionURLLength)
	}
	if utf8.RuneCountInString(item.OtherPlaceholder) > MaxQuestionPlaceholderLength {
		return QuestionItem{}, invalidQuestion("other_placeholder is longer than %d characters", MaxQuestionPlaceholderLength)
	}
	if len(raw.Options) > MaxQuestionOptions {
		return QuestionItem{}, invalidQuestion("options has more than %d choices", MaxQuestionOptions)
	}
	seenLabels := map[string]bool{}
	for _, rawOption := range raw.Options {
		option := QuestionOption{Label: strings.TrimSpace(rawOption.Label), Description: strings.TrimSpace(rawOption.Description)}
		if option.Label == "" || utf8.RuneCountInString(option.Label) > MaxQuestionOptionLabelLength {
			return QuestionItem{}, invalidQuestion("option labels must have 1 to %d characters", MaxQuestionOptionLabelLength)
		}
		if utf8.RuneCountInString(option.Description) > MaxQuestionOptionDescription {
			return QuestionItem{}, invalidQuestion("option descriptions are limited to %d characters", MaxQuestionOptionDescription)
		}
		key := strings.ToLower(option.Label)
		if seenLabels[key] {
			return QuestionItem{}, invalidQuestion("option label %q is repeated", option.Label)
		}
		seenLabels[key] = true
		item.Options = append(item.Options, option)
	}
	if item.MultiSelect && len(item.Options) < 2 {
		return QuestionItem{}, invalidQuestion("multi_select needs at least two options")
	}
	return item, nil
}

func safeQuestionURL(raw string) bool {
	if len(raw) > MaxQuestionURLLength {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

// NormalizeQuestionAnswers validates answers against the question's items and
// returns them keyed by item ID with declared option labels in canonical form.
func NormalizeQuestionAnswers(items []QuestionItem, answers map[string][]string) (map[string][]string, error) {
	known := map[string]bool{}
	for _, item := range items {
		known[item.ID] = true
	}
	for id := range answers {
		if !known[id] {
			return nil, invalidQuestion("answers contains unknown question %q", id)
		}
	}
	normalized := make(map[string][]string, len(items))
	for _, item := range items {
		values := answers[item.ID]
		if len(values) == 0 {
			return nil, invalidQuestion("question %q requires an answer", item.ID)
		}
		if !item.MultiSelect && len(values) > 1 {
			return nil, invalidQuestion("question %q allows one answer", item.ID)
		}
		freeText := 0
		var canonical []string
		for _, raw := range values {
			value := strings.TrimSpace(raw)
			if value == "" {
				return nil, invalidQuestion("question %q contains an empty answer", item.ID)
			}
			if label, ok := declaredQuestionOption(item, value); ok {
				value = label
			} else {
				if len(item.Options) > 0 && !item.AllowOther {
					return nil, invalidQuestion("question %q only accepts its listed options", item.ID)
				}
				freeText++
				if freeText > 1 {
					return nil, invalidQuestion("question %q accepts one free-text answer", item.ID)
				}
				if utf8.RuneCountInString(value) > MaxQuestionFreeTextLength {
					return nil, invalidQuestion("answers are limited to %d characters", MaxQuestionFreeTextLength)
				}
			}
			if slices.Contains(canonical, value) {
				continue
			}
			canonical = append(canonical, value)
		}
		normalized[item.ID] = canonical
	}
	return normalized, nil
}

func declaredQuestionOption(item QuestionItem, value string) (string, bool) {
	for _, option := range item.Options {
		if option.Label == value {
			return option.Label, true
		}
	}
	return "", false
}

// EffectiveQuestionStatus reports an open question past its deadline as expired.
func EffectiveQuestionStatus(status, expiresAt string, now time.Time) string {
	if status != QuestionStatusOpen {
		return status
	}
	deadline, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err == nil && !now.Before(deadline) {
		return QuestionStatusExpired
	}
	return status
}

// QuestionResolutionAllowed reports whether the authoring bot may move a
// question from its stored status to the requested one.
func QuestionResolutionAllowed(from, to string) bool {
	switch from {
	case QuestionStatusOpen:
		return to == QuestionStatusAnswered || to == QuestionStatusCancelled || to == QuestionStatusExpired || to == QuestionStatusFailed
	case QuestionStatusSubmitted:
		return to == QuestionStatusAnswered || to == QuestionStatusCancelled || to == QuestionStatusFailed || to == QuestionStatusOpen
	default:
		return false
	}
}

// IsTerminalQuestionStatus reports whether the question has a final outcome.
func IsTerminalQuestionStatus(status string) bool {
	return status == QuestionStatusAnswered || status == QuestionStatusCancelled || status == QuestionStatusExpired || status == QuestionStatusFailed
}
