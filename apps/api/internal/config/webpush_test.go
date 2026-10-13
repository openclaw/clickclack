package config

import (
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/webpush"
)

func TestServeRejectsInvalidWebPushKeys(t *testing.T) {
	t.Parallel()
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	otherPublic, _, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, public, private string }{
		{"malformed public", "not-a-point", private},
		{"malformed private", public, "not-a-scalar"},
		{"mismatched pair", otherPublic, private},
		{"missing private", public, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{PublicURL: "https://chat.example.com", WebPushVAPIDPublicKey: tc.public, WebPushVAPIDPrivateKey: tc.private}
			err := cfg.ValidateServe()
			if err == nil {
				t.Fatal("invalid VAPID keys accepted")
			}
			if tc.private != "" && strings.Contains(err.Error(), tc.private) {
				t.Fatal("configuration error exposes the private key")
			}
		})
	}
	cfg := Config{PublicURL: "https://chat.example.com", WebPushVAPIDPublicKey: public, WebPushVAPIDPrivateKey: private}
	if err := cfg.ValidateServe(); err != nil {
		t.Fatal(err)
	}
	if !cfg.WebPushEnabled() {
		t.Fatal("valid pair did not enable push")
	}
}
