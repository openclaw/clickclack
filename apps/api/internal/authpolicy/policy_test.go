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
		"":                               "",
		"https://Chat.Example.com":       "https://chat.example.com",
		"https://chat.example.com:443/":  "https://chat.example.com",
		"https://chat.example.com:0443/": "https://chat.example.com",
		"https://chat.example.com:8443":  "https://chat.example.com:8443",
		"https://chat.example.com:08443": "https://chat.example.com:8443",
		"http://localhost:8080/":         "http://localhost:8080",
		"http://127.0.0.1:8080":          "http://127.0.0.1:8080",
		"http://[::1]:8080":              "http://[::1]:8080",
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
		"https://chat.example.com:0",
		"https://chat.example.com:65536",
	} {
		if _, err := CanonicalPublicURL(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestCanonicalPublicAPIURL(t *testing.T) {
	t.Parallel()
	for input, expected := range map[string]string{
		"":                             "",
		"https://API.Example.com:443/": "https://api.example.com",
		"https://api.example.com/services/clickclack":  "https://api.example.com/services/clickclack",
		"https://api.example.com/services/clickclack/": "https://api.example.com/services/clickclack",
		"http://localhost:8081/clickclack":             "http://localhost:8081/clickclack",
	} {
		got, err := CanonicalPublicAPIURL(input)
		if err != nil {
			t.Fatalf("canonicalize %q: %v", input, err)
		}
		if got != expected {
			t.Fatalf("canonicalize %q: got %q, want %q", input, got, expected)
		}
	}
	for _, value := range []string{
		"http://api.example.com",
		"https://user@api.example.com",
		"https://api.example.com/base?x=1",
		"https://api.example.com/base#fragment",
		"https://api.example.com/.",
		"https://api.example.com/base/..",
		"https://api.example.com//",
		"https://api.example.com/base//nested",
		"https://api.example.com/base/../nested",
		"https://api.example.com/base%2Fnested",
		"https://api.example.com/base%20nested",
		"https://api.example.com/base\\nested",
	} {
		if _, err := CanonicalPublicAPIURL(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestNewCookieNames(t *testing.T) {
	t.Parallel()
	if got, err := NewCookieNames("", "", ""); err != nil || got != DefaultCookieNames() {
		t.Fatalf("unexpected default cookie names: %#v %v", got, err)
	}
	secure, err := NewCookieNames("prod-2", "https://chat.example.com", "https://api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if secure.Session != "__Host-cc-prod-2-session" || secure.OAuthBinding != "__Host-cc-prod-2-oauth-binding" || !secure.Namespaced {
		t.Fatalf("unexpected secure names: %#v", secure)
	}
	pathMounted, err := NewCookieNames("prod-2", "https://chat.example.com", "https://api.example.com/services/clickclack")
	if err != nil {
		t.Fatal(err)
	}
	if pathMounted.Session != "__Secure-cc-prod-2-session" || pathMounted.OAuthBinding != "__Secure-cc-prod-2-oauth-binding" || !pathMounted.Namespaced {
		t.Fatalf("unexpected path-mounted secure names: %#v", pathMounted)
	}
	loopback, err := NewCookieNames("dev", "http://localhost:8080", "http://localhost:8081")
	if err != nil {
		t.Fatal(err)
	}
	if loopback.Session != "cc-dev-session" || loopback.OAuthBinding != "cc-dev-oauth-binding" || !loopback.Namespaced {
		t.Fatalf("unexpected loopback names: %#v", loopback)
	}
	if _, err := NewCookieNames("prod", "", ""); err == nil {
		t.Fatal("expected namespaced cookies without a public URL to fail")
	}
}
