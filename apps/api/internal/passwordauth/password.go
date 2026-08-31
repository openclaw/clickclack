// Package passwordauth hashes and verifies local account passwords with
// argon2id. Hashes are stored in the PHC string format so they stay
// self-describing: a hash written with today's cost parameters still verifies
// after the defaults below are raised.
package passwordauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	// MinPasswordLength follows NIST SP 800-63B for human-chosen secrets.
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

// ValidatePassword reports whether a candidate secret is acceptable to store.
func ValidatePassword(password string) error {
	switch {
	case len(password) < MinPasswordLength:
		return ErrPasswordTooShort
	case len(password) > MaxPasswordLength:
		return ErrPasswordTooLong
	default:
		return nil
	}
}

// Hash derives a new argon2id hash and returns it in PHC string format.
func Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, defaultParams.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return encode(defaultParams, salt, derive(defaultParams, salt, password)), nil
}

// Verify reports whether password matches the encoded hash. It compares in
// constant time and returns false (never an error) for a mismatch, so callers
// can treat any false result as a failed login.
func Verify(encoded, password string) (bool, error) {
	stored, salt, digest, err := decode(encoded)
	if err != nil {
		return false, err
	}
	if len(password) > MaxPasswordLength {
		return false, nil
	}
	candidate := derive(stored, salt, password)
	return subtle.ConstantTimeCompare(candidate, digest) == 1, nil
}

// VerifyDecoy performs the same key derivation Verify would against a fixed
// hash generated at startup. Callers use it when an identifier has no password
// on file so that unknown accounts cost the same wall time as known ones.
func VerifyDecoy(password string) {
	_, _ = Verify(decoyHash(), password)
}

var decoyHash = sync.OnceValue(func() string {
	salt := make([]byte, defaultParams.saltLength)
	if _, err := rand.Read(salt); err != nil {
		// A decoy only has to cost the right amount of time, never to stay
		// secret, so a zero salt is an acceptable last resort.
		salt = make([]byte, defaultParams.saltLength)
	}
	return encode(defaultParams, salt, derive(defaultParams, salt, "decoy"))
})

func derive(p params, salt []byte, password string) []byte {
	return argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
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
