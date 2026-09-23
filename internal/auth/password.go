// Package auth handles credentials and sessions.
//
// Two rules shape everything here. Passwords are never stored, logged, or
// returned — only Argon2id hashes, and those never leave this package. And a
// failed login reveals nothing about whether the account exists: the response,
// the status code and the time taken are identical either way.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, from
// docs/roadmap/non-functional-requirements.md#4-security.
//
// Memory is the parameter that matters against GPU attack: time cost is cheap
// to parallelize, memory is not. 64 MiB per hash is the cost of making a
// large-scale offline crack expensive, and it is also why login is rate
// limited — an unauthenticated endpoint that allocates 64 MiB per call is a
// denial-of-service surface if left open.
const (
	argonMemoryKiB = 64 * 1024 // 64 MiB
	argonTime      = 3
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// argonParallelism is capped at 4: more threads than cores buys nothing, and
// on a small container it makes each hash slower rather than faster.
func argonParallelism() uint8 {
	p := runtime.NumCPU()
	if p > 4 {
		p = 4
	}

	if p < 1 {
		p = 1
	}

	return uint8(p) //nolint:gosec // bounded to 1..4 immediately above
}

// Errors returned when a hash cannot be used.
var (
	// ErrInvalidHash means the stored value is not a hash this package wrote.
	ErrInvalidHash = errors.New("auth: malformed password hash")

	// ErrIncompatibleVersion means the hash was produced by a different
	// Argon2 version than this build understands.
	ErrIncompatibleVersion = errors.New("auth: incompatible argon2 version")

	// ErrPasswordTooShort is returned when a password fails the length floor.
	ErrPasswordTooShort = errors.New("auth: password is too short")

	// ErrPasswordTooLong is returned when a password exceeds the ceiling.
	//
	// A sentinel like its short counterpart, so an HTTP layer can report both
	// as what they are: a person typed something the rules do not allow. Left
	// as a bare error it reaches the caller as an unexplained 500, which is
	// how "my password manager generated something too long" becomes a bug
	// report about the server being broken.
	ErrPasswordTooLong = errors.New("auth: password is too long")
)

// MinPasswordLength is the floor.
//
// Length is the only property enforced. Composition rules — a digit, a symbol,
// mixed case — push people toward "Password1!" and away from length, which is
// what actually matters. NIST dropped them for the same reason.
const MinPasswordLength = 12

// MaxPasswordLength bounds input before hashing. Argon2 will happily hash a
// megabyte, which is a cheap way to make an unauthenticated endpoint expensive.
const MaxPasswordLength = 1024

// HashPassword returns an encoded Argon2id hash.
//
// The encoding is the standard PHC string format, which carries the parameters
// alongside the digest. That is what lets the cost be raised later without
// invalidating existing hashes: an old hash still verifies with its own
// parameters, and [NeedsRehash] reports that it should be upgraded.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("%w: need at least %d characters", ErrPasswordTooShort, MinPasswordLength)
	}

	if len(password) > MaxPasswordLength {
		return "", fmt.Errorf("%w: at most %d bytes", ErrPasswordTooLong, MaxPasswordLength)
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	parallelism := argonParallelism()
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, parallelism, argonKeyLen)

	return encodeHash(salt, key, argonMemoryKiB, argonTime, parallelism), nil
}

// VerifyPassword reports whether a password matches an encoded hash.
//
// The comparison is constant time. A byte-by-byte comparison leaks how much of
// the digest matched through timing, which is enough to reconstruct it.
func VerifyPassword(password, encoded string) (bool, error) {
	d, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), d.salt, d.times, d.memory, d.parallelism, uint32(len(d.key))) //nolint:gosec // the length of a 32-byte digest

	return subtle.ConstantTimeCompare(got, d.key) == 1, nil
}

// NeedsRehash reports whether a stored hash was produced with weaker
// parameters than the current ones, so it can be upgraded on next login.
func NeedsRehash(encoded string) bool {
	d, err := decodeHash(encoded)
	if err != nil {
		// Unreadable is worse than outdated: rehash it.
		return true
	}

	return d.memory < argonMemoryKiB || d.times < argonTime || d.parallelism < argonParallelism()
}

// encodeHash renders the PHC string format.
func encodeHash(salt, key []byte, memory, times uint32, parallelism uint8) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, times, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// decoded is a parsed PHC hash string.
type decoded struct {
	salt        []byte
	key         []byte
	memory      uint32
	times       uint32
	parallelism uint8
}

// decodeHash parses the PHC string format.
func decodeHash(encoded string) (decoded, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return decoded{}, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return decoded{}, ErrInvalidHash
	}

	if version != argon2.Version {
		return decoded{}, ErrIncompatibleVersion
	}

	var d decoded
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &d.memory, &d.times, &d.parallelism); err != nil {
		return decoded{}, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return decoded{}, ErrInvalidHash
	}

	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return decoded{}, ErrInvalidHash
	}

	d.salt, d.key = salt, key

	return d, nil
}

// dummyHash is verified against when an account does not exist.
//
// Without it, an unknown user returns in microseconds while a real one takes
// the full Argon2 cost, and that difference is a reliable user-enumeration
// oracle regardless of how carefully the response body is matched.
var dummyHash = func() string {
	h, err := HashPassword("a-password-nobody-will-ever-use-0000")
	if err != nil {
		panic("auth: cannot build the dummy hash: " + err.Error())
	}

	return h
}()

// SpendVerifyTime performs a throwaway verification so that a login for a
// nonexistent account costs the same as one for a real account.
func SpendVerifyTime(password string) {
	// Both results are deliberately consumed and discarded: the point is to
	// spend the time, not to learn anything. An error here could only mean the
	// package-level dummy hash is broken, which this package's own tests catch.
	//
	// The `if` looks pointless and is not: errcheck runs with check-blank, so
	// `_, _ = VerifyPassword(...)` is a lint failure. Consuming both values in
	// a condition that returns either way is the form that satisfies it
	// without pretending to handle something. Simplified once, put back.
	if ok, err := VerifyPassword(password, dummyHash); ok || err != nil {
		return
	}
}
