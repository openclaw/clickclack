package store

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// Characterization tests pinning NormalizeBotSetupCodeDefaults, the public
// API-boundary validator invoked from both the sqlite and postgres setup-code
// mint paths (internal/store/{sqlite,postgres}/setup_codes.go:85). The function
// shipped with no direct coverage, so its trim/dedup/bound/scope gates could
// regress silently. These cases drive the real exported function.

func TestNormalizeBotSetupCodeDefaults_TrimsAndDedupes(t *testing.T) {
	out, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{
		DefaultTo: "  alice  ",
		AllowFrom: []string{"  bob  ", "bob", "carol"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DefaultTo != "alice" {
		t.Errorf("DefaultTo not trimmed: got %q, want %q", out.DefaultTo, "alice")
	}
	// Entries are trimmed before comparison, so "  bob  " and "bob" collapse to
	// one, and first-seen order is preserved.
	want := []string{"bob", "carol"}
	if len(out.AllowFrom) != len(want) {
		t.Fatalf("AllowFrom not deduped: got %v, want %v", out.AllowFrom, want)
	}
	for i := range want {
		if out.AllowFrom[i] != want[i] {
			t.Errorf("AllowFrom[%d] = %q, want %q (order/dedup)", i, out.AllowFrom[i], want[i])
		}
	}
}

func TestNormalizeBotSetupCodeDefaults_NilAllowFromStaysNil(t *testing.T) {
	// The `!= nil` guard means a nil slice must round-trip as nil, not become an
	// allocated empty slice.
	out, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{DefaultTo: "x"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AllowFrom != nil {
		t.Errorf("nil AllowFrom mutated to %#v, want nil", out.AllowFrom)
	}
}

func TestNormalizeBotSetupCodeDefaults_DefaultToLengthIsRuneCount(t *testing.T) {
	// The cap is measured in runes (utf8.RuneCountInString), not bytes: 256
	// two-byte runes (512 bytes) must pass, while 257 runes must fail. A
	// byte-length mutation would reject the 256-rune multibyte value.
	pass := strings.Repeat("é", maxBotSetupDefaultLength) // 256 runes, 512 bytes
	if _, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{DefaultTo: pass}, nil); err != nil {
		t.Errorf("256-rune multibyte DefaultTo rejected: %v", err)
	}
	tooLong := strings.Repeat("a", maxBotSetupDefaultLength+1) // 257 runes
	_, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{DefaultTo: tooLong}, nil)
	if err == nil || !strings.Contains(err.Error(), "defaultTo is too long") {
		t.Errorf("over-length DefaultTo: got err %v, want 'defaultTo is too long'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_DefaultToRejectsNUL(t *testing.T) {
	_, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{DefaultTo: "a\x00b"}, nil)
	if err == nil || !strings.Contains(err.Error(), "must not contain NUL") {
		t.Errorf("NUL in DefaultTo: got err %v, want 'must not contain NUL'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_DefaultToRejectsInvalidUTF8(t *testing.T) {
	_, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{DefaultTo: string([]byte{0xff, 0xfe})}, nil)
	if err == nil || !strings.Contains(err.Error(), "must be valid UTF-8") {
		t.Errorf("invalid UTF-8 DefaultTo: got err %v, want 'must be valid UTF-8'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_AllowFromTooManyEntries(t *testing.T) {
	// The count cap is checked against the raw input length before dedup, so
	// even duplicate entries beyond the limit are rejected.
	entries := make([]string, maxBotSetupAllowFrom+1)
	for i := range entries {
		entries[i] = "dup"
	}
	_, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{AllowFrom: entries}, nil)
	if err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Errorf("over-cap AllowFrom: got err %v, want 'too many entries'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_AllowFromAtCapPasses(t *testing.T) {
	// Exactly maxBotSetupAllowFrom unique entries is allowed (boundary: the
	// check is strictly greater-than).
	entries := make([]string, maxBotSetupAllowFrom)
	for i := range entries {
		entries[i] = "user-" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('0'+i%10)) + strings.Repeat("y", i/26)
	}
	out, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{AllowFrom: entries}, nil)
	if err != nil {
		t.Fatalf("at-cap AllowFrom rejected: %v", err)
	}
	if len(out.AllowFrom) != maxBotSetupAllowFrom {
		t.Errorf("at-cap AllowFrom length = %d, want %d", len(out.AllowFrom), maxBotSetupAllowFrom)
	}
}

func TestNormalizeBotSetupCodeDefaults_AllowFromRejectsEmptyAfterTrim(t *testing.T) {
	_, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{AllowFrom: []string{"  "}}, nil)
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("whitespace-only AllowFrom entry: got err %v, want 'must not be empty'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_AllowFromEntryValidated(t *testing.T) {
	// A non-empty but invalid entry is rejected by validateBotSetupDefault under
	// the "defaults.allowFrom entry" name.
	_, err := NormalizeBotSetupCodeDefaults(BotSetupCodeDefaults{AllowFrom: []string{"a\x00b"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "defaults.allowFrom entry must not contain NUL") {
		t.Errorf("NUL in AllowFrom entry: got err %v, want 'defaults.allowFrom entry must not contain NUL'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_AgentActivityRequiresScope(t *testing.T) {
	_, err := NormalizeBotSetupCodeDefaults(
		BotSetupCodeDefaults{AgentActivity: boolPtr(true)},
		[]string{"chat:write"},
	)
	if err == nil || !strings.Contains(err.Error(), "requires agent_activity:write") {
		t.Errorf("agentActivity without scope: got err %v, want 'requires agent_activity:write'", err)
	}
}

func TestNormalizeBotSetupCodeDefaults_AgentActivityWithScopePasses(t *testing.T) {
	out, err := NormalizeBotSetupCodeDefaults(
		BotSetupCodeDefaults{AgentActivity: boolPtr(true)},
		[]string{AgentActivityWriteScope},
	)
	if err != nil {
		t.Fatalf("agentActivity with scope rejected: %v", err)
	}
	if out.AgentActivity == nil || !*out.AgentActivity {
		t.Errorf("AgentActivity not preserved: got %v", out.AgentActivity)
	}
}

func TestNormalizeBotSetupCodeDefaults_AgentActivityFalseNeedsNoScope(t *testing.T) {
	// The gate fires only when the flag is present AND true; an explicit false
	// must pass without the scope.
	out, err := NormalizeBotSetupCodeDefaults(
		BotSetupCodeDefaults{AgentActivity: boolPtr(false)},
		nil,
	)
	if err != nil {
		t.Fatalf("agentActivity=false without scope rejected: %v", err)
	}
	if out.AgentActivity == nil || *out.AgentActivity {
		t.Errorf("AgentActivity=false not preserved: got %v", out.AgentActivity)
	}
}
