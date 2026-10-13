package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateKeysProducesAUsablePair(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	public, err := DecodeKey(publicKey)
	if err != nil || len(public) != 65 || public[0] != 4 {
		t.Fatalf("unexpected public key of %d bytes: %v", len(public), err)
	}
	private, err := DecodeKey(privateKey)
	if err != nil || len(private) != 32 {
		t.Fatalf("unexpected private key of %d bytes: %v", len(private), err)
	}
	if _, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), private); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(publicKey+privateKey, "+/=") {
		t.Fatal("keys must be unpadded base64url")
	}
}

func TestAuthorizationHeaderCarriesTheConfiguredSubject(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Now()
	for name, subject := range map[string]string{
		"mailto":     "mailto:ops@example.com",
		"https":      "https://chat.example.com",
		"bare email": "ops@example.com",
	} {
		sender := &Sender{PublicKey: publicKey, PrivateKey: privateKey, Subject: subject}
		header, err := sender.authorizationHeader("https://push.example.com/send/abc", issuedAt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		token, key, found := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
		if !found || key != publicKey {
			t.Fatalf("%s: unexpected header shape %q", name, header)
		}
		claims := jwt.MapClaims{}
		if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := subject
		if name == "bare email" {
			want = "mailto:" + subject
		}
		if claims["sub"] != want {
			t.Fatalf("%s: sub is %q, want %q", name, claims["sub"], want)
		}
		if claims["aud"] != "https://push.example.com" {
			t.Fatalf("%s: aud is %q", name, claims["aud"])
		}
		expiry, err := claims.GetExpirationTime()
		if err != nil || expiry.After(issuedAt.Add(24*time.Hour)) || !expiry.After(issuedAt) {
			t.Fatalf("%s: unexpected expiry %v: %v", name, expiry, err)
		}
	}
}

func TestAuthorizationHeaderVerifiesWithThePublicKey(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	sender := &Sender{PublicKey: publicKey, PrivateKey: privateKey, Subject: "https://chat.example.com"}
	header, err := sender.authorizationHeader("https://push.example.com/send/abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	raw, err := DecodeKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	verificationKey, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return verificationKey, nil }, jwt.WithValidMethods([]string{"ES256"}))
	if err != nil || !parsed.Valid {
		t.Fatalf("token did not verify: %v", err)
	}
}

func TestAuthorizationHeaderRejectsBadConfiguration(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	valid := Sender{PublicKey: publicKey, PrivateKey: privateKey, Subject: "https://chat.example.com"}
	for name, sender := range map[string]Sender{
		"no subject":      {PublicKey: publicKey, PrivateKey: privateKey},
		"opaque subject":  {PublicKey: publicKey, PrivateKey: privateKey, Subject: "support desk"},
		"bad public key":  {PublicKey: "not base64!", PrivateKey: privateKey, Subject: valid.Subject},
		"bad private key": {PublicKey: publicKey, PrivateKey: "AAAA", Subject: valid.Subject},
	} {
		if _, err := sender.authorizationHeader("https://push.example.com/send/abc", time.Now()); err == nil {
			t.Fatalf("expected %s to fail", name)
		}
	}
	if _, err := valid.authorizationHeader("://nonsense", time.Now()); err == nil {
		t.Fatal("expected an unparsable endpoint to fail")
	}
}

func TestDecodeKeyAcceptsCommonEncodings(t *testing.T) {
	t.Parallel()
	raw := []byte{0xfb, 0xff, 0x00, 0x11}
	for name, encoded := range map[string]string{
		"raw url":  base64.RawURLEncoding.EncodeToString(raw),
		"padded":   base64.URLEncoding.EncodeToString(raw),
		"standard": base64.StdEncoding.EncodeToString(raw),
	} {
		decoded, err := DecodeKey(encoded)
		if err != nil || string(decoded) != string(raw) {
			t.Fatalf("%s: %x %v", name, decoded, err)
		}
	}
	if _, err := DecodeKey("   "); err == nil {
		t.Fatal("expected an empty key to fail")
	}
}

func TestKeyFingerprintNamesTheKeyNotItsSpelling(t *testing.T) {
	t.Parallel()
	first, _, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := KeyFingerprint(first)
	if len(fingerprint) != 12 {
		t.Fatalf("fingerprint %q is not twelve hex digits", fingerprint)
	}
	raw, err := DecodeKey(first)
	if err != nil {
		t.Fatal(err)
	}
	if padded := base64.StdEncoding.EncodeToString(raw); KeyFingerprint(padded) != fingerprint {
		t.Fatal("a padded standard spelling of the same key must fingerprint the same")
	}
	if KeyFingerprint(second) == fingerprint {
		t.Fatal("a rotated key must fingerprint differently")
	}
}
