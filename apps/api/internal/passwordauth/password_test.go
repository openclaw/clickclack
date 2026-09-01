package passwordauth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHashAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	hash, err := Hash(t.Context(), "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("expected a PHC argon2id encoding, got %q", hash)
	}
	matched, err := Verify(t.Context(), hash, "correct horse battery")
	if err != nil || !matched {
		t.Fatalf("expected the original password to verify, got matched=%v err=%v", matched, err)
	}
}

func TestHashUsesAFreshSaltPerCall(t *testing.T) {
	t.Parallel()
	first, err := Hash(t.Context(), "same password twice")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash(t.Context(), "same password twice")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected two hashes of one password to differ")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	t.Parallel()
	hash, err := Hash(t.Context(), "the real password")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{"the real passwore", "", "THE REAL PASSWORD", strings.Repeat("x", MaxPasswordLength+1)} {
		matched, err := Verify(t.Context(), hash, candidate)
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
	valid, err := Hash(t.Context(), "a valid password")
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
		if _, err := Verify(t.Context(), encoded, "a valid password"); err == nil {
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
	if _, err := Hash(t.Context(), "short"); err == nil {
		t.Fatal("expected Hash to enforce the minimum length")
	}
}

func TestPasswordLengthCountsUnicodeCharacters(t *testing.T) {
	for _, input := range []struct {
		value string
		valid bool
	}{
		{strings.Repeat("🦞", 4), false},
		{strings.Repeat("🦞", MinPasswordLength), true},
		{strings.Repeat("🦞", MaxPasswordLength), true},
	} {
		if err := ValidatePassword(input.value); (err == nil) != input.valid {
			t.Errorf("length %d bytes: valid=%v, error=%v", len(input.value), input.valid, err)
		}
	}
}

// Signal when admission evaluates cancellation, without timing an Argon2 run.
type admissionContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *admissionContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestDerivationBudgetAdmission(t *testing.T) {
	// Stay sequential: these tests occupy the process-wide slots themselves.
	if cap(derivationSlots) != 2 {
		t.Fatalf("derivation capacity = %d, want 2", cap(derivationSlots))
	}
	hash, err := Hash(t.Context(), "a synthetic password")
	if err != nil {
		t.Fatal(err)
	}
	operations := map[string]func(context.Context) error{
		"hash": func(ctx context.Context) error {
			_, err := Hash(ctx, "a synthetic password")
			return err
		},
		"verify": func(ctx context.Context) error {
			matched, err := Verify(ctx, hash, "a synthetic password")
			if err == nil && !matched {
				return errors.New("valid password did not match")
			}
			return err
		},
		"decoy": func(ctx context.Context) error { return VerifyDecoy(ctx, "synthetic guess") },
	}
	for name, operation := range operations {
		for _, cancelQueued := range []bool{false, true} {
			outcome := "resumes"
			if cancelQueued {
				outcome = "cancels"
			}
			t.Run(name+"/"+outcome, func(t *testing.T) {
				for range cap(derivationSlots) {
					derivationSlots <- struct{}{}
				}
				held := cap(derivationSlots)
				defer func() {
					for range held {
						<-derivationSlots
					}
				}()
				base, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx := &admissionContext{Context: base, waiting: make(chan struct{})}
				done := make(chan error, 1)
				go func() { done <- operation(ctx) }()
				watchdog := time.NewTimer(10 * time.Second)
				defer watchdog.Stop()
				select {
				case <-ctx.waiting:
				case <-watchdog.C:
					t.Fatal("operation did not reach shared budget admission")
				}
				select {
				case err := <-done:
					t.Fatalf("operation bypassed the saturated budget: %v", err)
				default:
				}
				var want error
				if cancelQueued {
					cancel()
					want = context.Canceled
				} else {
					<-derivationSlots
					held--
				}
				select {
				case err := <-done:
					if !errors.Is(err, want) {
						t.Fatalf("operation returned %v, want %v", err, want)
					}
				case <-watchdog.C:
					t.Fatal("operation did not resume or cancel")
				}
				if got := len(derivationSlots); got != held {
					t.Fatalf("occupied slots = %d, want %d; operation leaked or stole a slot", got, held)
				}
			})
		}
		t.Run(name+"/expired", func(t *testing.T) {
			ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			defer cancel()
			if err := operation(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("operation returned %v, want expired deadline", err)
			}
			if got := len(derivationSlots); got != 0 {
				t.Fatalf("expired operation leaked %d slots", got)
			}
		})
	}
}
