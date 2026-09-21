package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/auth"
)

const goodPassword = "correct horse battery staple"

func TestHashAndVerify(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := auth.VerifyPassword(goodPassword, hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}

	if !ok {
		t.Error("the correct password did not verify")
	}

	ok, err = auth.VerifyPassword("wrong password entirely", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}

	if ok {
		t.Error("a wrong password verified")
	}
}

// The hash must never contain the password, in any form. This is the property
// that makes a database dump survivable.
func TestHashDoesNotContainPassword(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if strings.Contains(hash, goodPassword) {
		t.Fatalf("the hash contains the password: %s", hash)
	}

	for _, word := range strings.Fields(goodPassword) {
		if strings.Contains(hash, word) {
			t.Errorf("the hash contains %q from the password", word)
		}
	}
}

// Salting means the same password hashes differently every time. Without it,
// identical passwords are visibly identical in a dump, and a precomputed table
// cracks them all at once.
func TestHashesAreSalted(t *testing.T) {
	t.Parallel()

	first, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	second, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if first == second {
		t.Error("hashing the same password twice produced identical output; it is not salted")
	}

	// Both must still verify.
	for i, h := range []string{first, second} {
		ok, verr := auth.VerifyPassword(goodPassword, h)
		if verr != nil || !ok {
			t.Errorf("hash %d did not verify: ok=%v err=%v", i, ok, verr)
		}
	}
}

// The encoding carries its own parameters, which is what lets the cost be
// raised later without invalidating existing hashes.
func TestHashFormatIsPHC(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want the argon2id PHC prefix", hash)
	}

	for _, part := range []string{"m=", "t=", "p=", "v="} {
		if !strings.Contains(hash, part) {
			t.Errorf("hash %q does not record %q", hash, part)
		}
	}
}

func TestPasswordLengthIsEnforced(t *testing.T) {
	t.Parallel()

	short := strings.Repeat("a", auth.MinPasswordLength-1)

	if _, err := auth.HashPassword(short); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("a short password gave %v, want ErrPasswordTooShort", err)
	}

	// Argon2 will happily hash a megabyte, which is a cheap way to make an
	// unauthenticated endpoint expensive.
	huge := strings.Repeat("a", auth.MaxPasswordLength+1)
	if _, err := auth.HashPassword(huge); err == nil {
		t.Error("an oversized password was accepted")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	t.Parallel()

	for name, hash := range map[string]string{
		"empty":          "",
		"not a hash":     "hunter2",
		"wrong scheme":   "$bcrypt$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		"missing parts":  "$argon2id$v=19$m=65536,t=3,p=4",
		"bad base64":     "$argon2id$v=19$m=65536,t=3,p=4$!!!!$!!!!",
		"bad parameters": "$argon2id$v=19$nonsense$c2FsdA$aGFzaA",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := auth.VerifyPassword(goodPassword, hash); err == nil {
				t.Errorf("a malformed hash %q was accepted", hash)
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	t.Parallel()

	current, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if auth.NeedsRehash(current) {
		t.Error("a freshly created hash wants rehashing")
	}

	// A hash made with weaker parameters should be upgraded on next login.
	weak := "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0c2E$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGE"
	if !auth.NeedsRehash(weak) {
		t.Error("a weak hash does not want rehashing")
	}

	// An unreadable hash is worse than outdated.
	if !auth.NeedsRehash("garbage") {
		t.Error("an unreadable hash does not want rehashing")
	}
}

// A nonexistent account must cost roughly what a real one does, or the timing
// difference is a user-enumeration oracle regardless of how carefully the
// response body is matched.
func TestSpendVerifyTimeCostsTheSameAsARealVerification(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	realStart := time.Now()
	_, _ = auth.VerifyPassword("some attempt", hash)
	realCost := time.Since(realStart)

	dummyStart := time.Now()
	auth.SpendVerifyTime("some attempt")
	dummyCost := time.Since(dummyStart)

	// Wide tolerance: this asserts the same order of magnitude, not a precise
	// match. A dummy that returned instantly would be off by several.
	ratio := float64(dummyCost) / float64(realCost)
	if ratio < 0.25 || ratio > 4 {
		t.Errorf("dummy verification cost %v vs real %v (ratio %.2f); "+
			"the timing difference is an enumeration oracle", dummyCost, realCost, ratio)
	}
}

// --- tokens ---------------------------------------------------------------

func TestTokensAreUniqueAndOpaque(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 1000 {
		token, err := auth.NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}

		if seen[token] {
			t.Fatalf("NewToken returned a duplicate: %s", token)
		}

		seen[token] = true

		// 32 bytes base64url without padding.
		if len(token) != 43 {
			t.Errorf("token length = %d, want 43 (256 bits)", len(token))
		}
	}
}

// The stored value must not be the token: a database dump must not hand the
// reader a set of working sessions.
func TestTokenHashIsNotTheToken(t *testing.T) {
	t.Parallel()

	token, err := auth.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	hash := auth.HashToken(token)

	if hash == token {
		t.Fatal("HashToken returned the token unchanged")
	}

	if strings.Contains(hash, token) {
		t.Fatal("the hash contains the token")
	}

	// Deterministic, so lookup by hash works.
	if auth.HashToken(token) != hash {
		t.Error("HashToken is not deterministic")
	}

	// Distinct tokens hash distinctly.
	other, _ := auth.NewToken()
	if auth.HashToken(other) == hash {
		t.Error("two different tokens produced the same hash")
	}
}
