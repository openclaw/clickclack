package authpolicy

import "testing"

func TestParseCookieNamespace(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "a", "prod", "prod-2", "a1-b2", "abcdefghijklmnopqrstuvwxyz123456"} {
		if _, err := ParseCookieNamespace(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}
	for _, value := range []string{"Prod", "-prod", "prod-", "prod_name", "prod.name", "prod/name", "abcdefghijklmnopqrstuvwxyz1234567"} {
		if _, err := ParseCookieNamespace(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestCanonicalPublicURL(t *testing.T) {
	t.Parallel()
	for input, expected := range map[string]string{
		"":                              "",
		"https://Chat.Example.com":      "https://chat.example.com",
		"https://chat.example.com:443/": "https://chat.example.com",
		"https://chat.example.com:8443": "https://chat.example.com:8443",
		"http://localhost:8080/":        "http://localhost:8080",
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080",
		"http://[::1]:8080":             "http://[::1]:8080",
	} {
		got, err := CanonicalPublicURL(input)
		if err != nil {
			t.Fatalf("canonicalize %q: %v", input, err)
		}
		if got != expected {
			t.Fatalf("canonicalize %q: got %q, want %q", input, got, expected)
		}
	}
	for _, value := range []string{
		"ftp://chat.example.com",
		"https://",
		"https://user:secret@chat.example.com",
		"https://chat.example.com/app",
		"https://chat.example.com?x=1",
		"https://chat.example.com#fragment",
		"http://chat.example.com",
		"https://chat.example.com.",
	} {
		if _, err := CanonicalPublicURL(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestNewCookieNames(t *testing.T) {
	t.Parallel()
	if got, err := NewCookieNames("", ""); err != nil || got != DefaultCookieNames() {
		t.Fatalf("unexpected default cookie names: %#v %v", got, err)
	}
	secure, err := NewCookieNames("prod-2", "https://chat.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if secure.Session != "__Host-cc-prod-2-session" || secure.OAuthBinding != "__Host-cc-prod-2-oauth-binding" {
		t.Fatalf("unexpected secure names: %#v", secure)
	}
	loopback, err := NewCookieNames("dev", "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if loopback.Session != "cc-dev-session" || loopback.OAuthBinding != "cc-dev-oauth-binding" {
		t.Fatalf("unexpected loopback names: %#v", loopback)
	}
	if _, err := NewCookieNames("prod", ""); err == nil {
		t.Fatal("expected namespaced cookies without a public URL to fail")
	}
}
