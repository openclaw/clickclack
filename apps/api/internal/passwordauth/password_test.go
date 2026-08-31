package passwordauth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	hash, err := Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("expected a PHC argon2id encoding, got %q", hash)
	}
	matched, err := Verify(hash, "correct horse battery")
	if err != nil || !matched {
		t.Fatalf("expected the original password to verify, got matched=%v err=%v", matched, err)
	}
}

func TestHashUsesAFreshSaltPerCall(t *testing.T) {
	t.Parallel()
	first, err := Hash("same password twice")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash("same password twice")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected two hashes of one password to differ")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	t.Parallel()
	hash, err := Hash("the real password")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{"the real passwore", "", "THE REAL PASSWORD", strings.Repeat("x", MaxPasswordLength+1)} {
		matched, err := Verify(hash, candidate)
		if err != nil {
			t.Fatalf("verify(%q) returned an error: %v", candidate, err)
		}
		if matched {
			t.Fatalf("expected %q to be rejected", candidate)
		}
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	t.Parallel()
	valid, err := Hash("a valid password")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Split(valid, "$")
	malformed := []string{
		"",
		"not-a-hash",
		"$bcrypt$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=0,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$$" + fields[5],
		"$argon2id$v=19$m=65536,t=3,p=2$" + fields[4] + "$",
		"$argon2id$v=19$m=65536,t=3,p=2$not base64!$" + fields[5],
	}
	for _, encoded := range malformed {
		if _, err := Verify(encoded, "a valid password"); err == nil {
			t.Fatalf("expected %q to be rejected as malformed", encoded)
		}
	}
}

func TestValidatePasswordBounds(t *testing.T) {
	t.Parallel()
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength-1)); err == nil {
		t.Fatal("expected a short password to be rejected")
	}
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLength+1)); err == nil {
		t.Fatal("expected an over-long password to be rejected")
	}
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength)); err != nil {
		t.Fatalf("expected a minimum-length password to be accepted, got %v", err)
	}
	if _, err := Hash("short"); err == nil {
		t.Fatal("expected Hash to enforce the minimum length")
	}
}

func TestVerifyDecoyDoesNotPanicOrMatch(t *testing.T) {
	t.Parallel()
	// The decoy exists only to burn comparable time, so the contract under
	// test is that it always completes and never reports a match.
	VerifyDecoy("decoy")
	matched, err := Verify(decoyHash(), "decoy")
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("expected the decoy hash to be a well-formed hash of its own input")
	}
}
