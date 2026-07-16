package store

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxBotSetupDefaultLength = 256
	maxBotSetupAllowFrom     = 500
)

// NormalizeBotSetupCodeDefaults validates the small configuration payload
// carried by a setup-code grant. It is consumed by another trusted client,
// but still crosses the public API boundary and must remain bounded.
func NormalizeBotSetupCodeDefaults(defaults BotSetupCodeDefaults, scopes []string) (BotSetupCodeDefaults, error) {
	defaults.DefaultTo = strings.TrimSpace(defaults.DefaultTo)
	if err := validateBotSetupDefault("defaultTo", defaults.DefaultTo); err != nil {
		return BotSetupCodeDefaults{}, err
	}
	if len(defaults.AllowFrom) > maxBotSetupAllowFrom {
		return BotSetupCodeDefaults{}, errors.New("defaults.allowFrom has too many entries")
	}
	if defaults.AllowFrom != nil {
		normalized := make([]string, 0, len(defaults.AllowFrom))
		seen := make(map[string]struct{}, len(defaults.AllowFrom))
		for _, value := range defaults.AllowFrom {
			value = strings.TrimSpace(value)
			if value == "" {
				return BotSetupCodeDefaults{}, errors.New("defaults.allowFrom entries must not be empty")
			}
			if err := validateBotSetupDefault("defaults.allowFrom entry", value); err != nil {
				return BotSetupCodeDefaults{}, err
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			normalized = append(normalized, value)
		}
		defaults.AllowFrom = normalized
	}
	if defaults.AgentActivity != nil && *defaults.AgentActivity &&
		!slices.Contains(scopes, AgentActivityWriteScope) {
		return BotSetupCodeDefaults{}, errors.New("defaults.agentActivity requires agent_activity:write")
	}
	return defaults, nil
}

func validateBotSetupDefault(name, value string) error {
	if !utf8.ValidString(value) {
		return errors.New(name + " must be valid UTF-8")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return errors.New(name + " must not contain NUL")
	}
	if utf8.RuneCountInString(value) > maxBotSetupDefaultLength {
		return errors.New(name + " is too long")
	}
	return nil
}
