package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// TokenBytes is the entropy in a session token.
//
// 256 bits, per docs/roadmap/non-functional-requirements.md#4-security. A
// session token is a bearer credential: anyone holding it is the user, so its
// only defense is being unguessable.
const TokenBytes = 32

// NewToken returns a fresh session token.
//
// The value is returned once, handed to the client, and never stored. What is
// stored is [HashToken] of it, so a database dump does not yield a set of
// working sessions.
func NewToken() (string, error) {
	buf := make([]byte, TokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the value stored for a token.
//
// A plain SHA-256, not a password hash. Argon2 exists to make guessing a
// low-entropy secret expensive; a 256-bit random token cannot be guessed, so
// the slow hash would buy nothing and would add its cost to every single
// authenticated request.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
