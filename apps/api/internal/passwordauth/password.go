// Package passwordauth hashes and verifies local account passwords with
// argon2id. Hashes are stored in the PHC string format so they stay
// self-describing: a hash written with today's cost parameters still verifies
// after the defaults below are raised.
package passwordauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	MinPasswordLength = 8
	// MaxPasswordLength bounds the work an unauthenticated caller can ask for.
	MaxPasswordLength = 256
)

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d characters", MaxPasswordLength)
	ErrInvalidHash      = errors.New("password hash is not a supported argon2id encoding")
)

// params are the argon2id cost parameters used for new hashes. Verification
// reads the parameters recorded in the stored hash instead of these.
type params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = params{memory: 64 * 1024, iterations: 3, parallelism: 2, saltLength: 16, keyLength: 32}

// Share the budget across all password entrypoints: 128 MiB at current costs.
var derivationSlots = make(chan struct{}, 2)

// ValidatePassword reports whether a candidate secret is acceptable to store.
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	switch {
	case length < MinPasswordLength:
		return ErrPasswordTooShort
	case length > MaxPasswordLength:
		return ErrPasswordTooLong
	default:
		return nil
	}
}

// Hash derives a new argon2id hash and returns it in PHC string format.
func Hash(ctx context.Context, password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, defaultParams.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest, err := derive(ctx, defaultParams, salt, password)
	if err != nil {
		return "", err
	}
	return encode(defaultParams, salt, digest), nil
}

// Verify reports whether password matches the encoded hash. It compares in
// constant time; mismatches return false without an error, while cancellation
// and invalid hashes return an error rather than a failed guess.
func Verify(ctx context.Context, encoded, password string) (bool, error) {
	stored, salt, digest, err := decode(encoded)
	if err != nil {
		return false, err
	}
	if utf8.RuneCountInString(password) > MaxPasswordLength {
		return false, nil
	}
	candidate, err := derive(ctx, stored, salt, password)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(candidate, digest) == 1, nil
}

// VerifyDecoy pays the same derivation cost for unknown or unenrolled accounts.
func VerifyDecoy(ctx context.Context, password string) error {
	if utf8.RuneCountInString(password) > MaxPasswordLength {
		return nil
	}
	// The discarded digest is never a credential, so its salt need not be secret
	// or unique. Direct derivation also keeps cold requests cancellable in queue.
	_, err := derive(ctx, defaultParams, make([]byte, defaultParams.saltLength), password)
	return err
}

func derive(ctx context.Context, p params, salt []byte, password string) ([]byte, error) {
	select {
	case derivationSlots <- struct{}{}:
		defer func() { <-derivationSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Cancellation and an available slot can race in the select above.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Argon2 cannot be interrupted; keep the slot until its memory is no longer
	// in use, even when the caller cancels during the derivation.
	digest := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
	return digest, ctx.Err()
}

func encode(p params, salt, digest []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.iterations, p.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest))
}

func decode(encoded string) (params, []byte, []byte, error) {
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" {
		return params{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(fields[2], "v=%d", &version); err != nil || version != argon2.Version {
		return params{}, nil, nil, ErrInvalidHash
	}
	var p params
	if _, err := fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return params{}, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(fields[4])
	if err != nil || len(salt) == 0 {
		return params{}, nil, nil, ErrInvalidHash
	}
	digest, err := base64.RawStdEncoding.Strict().DecodeString(fields[5])
	if err != nil || len(digest) == 0 {
		return params{}, nil, nil, ErrInvalidHash
	}
	if p.memory == 0 || p.iterations == 0 || p.parallelism == 0 {
		return params{}, nil, nil, ErrInvalidHash
	}
	p.keyLength = uint32(len(digest))
	p.saltLength = uint32(len(salt))
	return p, salt, digest, nil
}
