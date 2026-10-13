package webpush

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// vapidTokenLifetime is how long a push service will accept one signed token.
// RFC 8292 caps it at 24 hours; half a day leaves room for clock skew.
const vapidTokenLifetime = 12 * time.Hour

// GenerateKeys returns a fresh base64url VAPID key pair, public key first. The
// public key is the uncompressed P-256 point the browser passes to
// pushManager.subscribe; the private key is the raw scalar.
func GenerateKeys() (publicKey, privateKey string, err error) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	encoding := base64.RawURLEncoding
	return encoding.EncodeToString(key.PublicKey().Bytes()), encoding.EncodeToString(key.Bytes()), nil
}

// ValidateKeys rejects malformed or mismatched VAPID identities before devices register.
func ValidateKeys(publicKey, privateKey string) error {
	public, err := DecodeKey(publicKey)
	if err != nil {
		return errors.New("vapid public key must be base64url")
	}
	if _, err := ecdh.P256().NewPublicKey(public); err != nil {
		return errors.New("vapid public key must be an uncompressed P-256 point")
	}
	private, err := DecodeKey(privateKey)
	if err != nil {
		return errors.New("vapid private key must be base64url")
	}
	key, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		return errors.New("vapid private key must be a valid P-256 scalar")
	}
	if !bytes.Equal(public, key.PublicKey().Bytes()) {
		return errors.New("vapid public and private keys do not match")
	}
	return nil
}

// authorizationHeader signs the RFC 8292 token that identifies this
// application server to one push service.
func (s *Sender) authorizationHeader(endpoint string, issuedAt time.Time) (string, error) {
	audience, err := pushServiceAudience(endpoint)
	if err != nil {
		return "", err
	}
	subject, err := NormalizeSubject(s.Subject)
	if err != nil {
		return "", err
	}
	publicKey, err := DecodeKey(s.PublicKey)
	if err != nil {
		return "", fmt.Errorf("vapid public key: %w", err)
	}
	privateKey, err := DecodeKey(s.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("vapid private key: %w", err)
	}
	signingKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), privateKey)
	if err != nil {
		return "", fmt.Errorf("vapid private key: %w", err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"aud": audience,
		"exp": issuedAt.Add(vapidTokenLifetime).Unix(),
		"sub": subject,
	}).SignedString(signingKey)
	if err != nil {
		return "", err
	}
	return "vapid t=" + token + ", k=" + base64.RawURLEncoding.EncodeToString(publicKey), nil
}

// NormalizeSubject returns the RFC 8292 contact for the application server. A
// value that already carries its scheme is kept byte for byte: push services
// reject a doubled "mailto:" prefix, and Apple rejects every push that carries
// one.
func NormalizeSubject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	switch {
	case subject == "":
		return "", errors.New("vapid subject is required")
	case strings.HasPrefix(subject, "mailto:"), strings.HasPrefix(subject, "https://"):
		return subject, nil
	case strings.Contains(subject, "@"):
		return "mailto:" + subject, nil
	default:
		return "", errors.New("vapid subject must be an https URL or a mailto address")
	}
}

// DecodeKey accepts the base64url a browser, an operator, or a config file is
// likely to carry: with or without padding, and with the standard alphabet.
func DecodeKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("key is empty")
	}
	value = strings.NewReplacer("+", "-", "/", "_").Replace(strings.TrimRight(value, "="))
	return base64.RawURLEncoding.DecodeString(value)
}

// KeyFingerprint names a VAPID public key in a log line: the first twelve hex
// digits of the SHA-256 of its bytes, so every spelling of one key agrees. The
// key is public; the fingerprint keeps the line short and changes when the
// operator rotates the pair.
func KeyFingerprint(publicKey string) string {
	raw, err := DecodeKey(publicKey)
	if err != nil {
		raw = []byte(strings.TrimSpace(publicKey))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

func pushServiceAudience(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("push endpoint must be an absolute URL")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
