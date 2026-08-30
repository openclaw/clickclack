package store

import "testing"

func TestIdentitySyncRejectsBotMapping(t *testing.T) {
	input := IdentitySyncInput{Source: "https://control.example.com", Profiles: []IdentitySyncProfile{{ID: "person", Emails: []string{"person@example.com"}}}}
	_, _, err := PlanIdentitySync(input, []IdentitySyncRow{{User: User{ID: "bot", Kind: "bot"}, Provider: "legacy", Subject: "bot", Email: "person@example.com"}})
	if err == nil {
		t.Fatal("an imported human identity must not target a bot")
	}
}

func TestIdentitySyncRejectsUntrustedSourceOrAliases(t *testing.T) {
	for _, source := range []string{"", "javascript:alert(1)", "https://user:password@example.com", "https://example.com/path", "https://example.com?query=1", "https://example.com#fragment"} {
		if _, err := NormalizeIdentitySync(IdentitySyncInput{Source: source, Profiles: []IdentitySyncProfile{}}); err == nil {
			t.Errorf("accepted source %q", source)
		}
	}
	_, err := NormalizeIdentitySync(IdentitySyncInput{Source: "https://example.com", Profiles: []IdentitySyncProfile{{ID: "one", Emails: []string{"alias@example.com"}}, {ID: "two", Emails: []string{"ALIAS@example.com"}}}})
	if err == nil {
		t.Fatal("accepted the same alias for different source profiles")
	}
}
