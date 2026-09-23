// Package secrets encrypts the values Pivot stores that must not be readable
// from the database alone: an OIDC client secret today, warehouse credentials
// in Phase 1.
//
// # Why envelope encryption
//
// Each value gets its own random data key, which encrypts the value; the data
// key is then wrapped by a master key and stored alongside the ciphertext.
// Encrypting directly with the master key would be simpler and would make
// rotation mean re-encrypting every secret with the new key. With envelopes,
// rotation rewraps data keys -- the same work whether there are five secrets or
// five thousand -- and no single key ever encrypts more than one value.
//
// # The threat this answers, and the one it does not
//
// It answers: somebody obtains the database file. A stolen backup, a snapshot
// left on object storage, a decommissioned disk, a `SELECT *` by somebody with
// read access to the metadata store. In every one of those the ciphertext is
// useless without the master key, which lives somewhere else.
//
// It does not answer: somebody obtains the machine. A process that can decrypt
// on demand holds the key in memory and reads the key file at startup, so root
// on that host can have the plaintext. Defending against that needs a KMS or an
// HSM, which Phase 9 can add behind this same interface -- the format carries a
// key identifier precisely so a future key source is a new implementation
// rather than a migration.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Errors a caller has to distinguish.
var (
	// ErrNoKey means nothing is configured to encrypt with.
	ErrNoKey = errors.New("secrets: no encryption key is configured")

	// ErrUnknownKey means a stored value was encrypted with a key this
	// instance does not have. Recoverable -- find the key -- which is why it
	// is distinct from a corrupt value.
	ErrUnknownKey = errors.New("secrets: the value was encrypted with a key this instance does not have")

	// ErrMalformed means the stored value is not a Pivot envelope.
	ErrMalformed = errors.New("secrets: the stored value is not readable as an encrypted secret")
)

// KeySize is the master key length. AES-256.
const KeySize = 32

/*
The envelope format.

	pivot.v1.<key id>.<wrapped data key>.<ciphertext>

Five dot-separated fields, each of the last two base64url without padding.

Self-describing on purpose. A value pulled out of the database says what it is,
which version of this format it uses and which key it needs -- so an operator
looking at a row can tell an encrypted secret from a plaintext one without
consulting anything, and a future format can be introduced beside this one
rather than instead of it.
*/
const (
	prefix  = "pivot"
	version = "v1"
	fields  = 5
)

// Cipher encrypts and decrypts stored values.
//
// An interface because the key source is going to change: a KMS, or a cloud
// provider's key service, is a different implementation of exactly this.
type Cipher interface {
	// Encrypt returns an envelope for plaintext.
	//
	// purpose binds the ciphertext to where it is stored -- it is
	// authenticated but not secret -- so a value lifted out of one column
	// cannot be pasted into another and decrypted there.
	Encrypt(purpose, plaintext string) (string, error)

	// Decrypt returns the plaintext, or the value unchanged when it is not an
	// envelope at all. See [Keyring.Decrypt] for why that case exists.
	Decrypt(purpose, stored string) (string, error)
}

// Key is a master key and its identifier.
type Key struct {
	// ID is a fingerprint of the key material, not a name somebody chose.
	//
	// Derived rather than configured so that two instances given the same key
	// agree on its identity without being told, and so an operator cannot
	// rotate the label while leaving the key -- or the reverse, which produces
	// envelopes that name a key whose material has changed underneath them.
	ID string

	material []byte
	aead     cipher.AEAD
}

// NewKey builds a key from raw material.
func NewKey(material []byte) (Key, error) {
	if len(material) != KeySize {
		return Key{}, fmt.Errorf("secrets: a key must be %d bytes, got %d", KeySize, len(material))
	}

	block, err := aes.NewCipher(material)
	if err != nil {
		return Key{}, fmt.Errorf("secrets: build cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Key{}, fmt.Errorf("secrets: build gcm: %w", err)
	}

	return Key{ID: fingerprint(material), material: material, aead: aead}, nil
}

// ParseKey reads a base64 key as configuration supplies it.
//
// Standard and URL alphabets, padded or not, because a key travels through
// shells, YAML files and secret managers and arrives in whichever form the last
// one favored. Rejecting a key for its punctuation helps nobody.
func ParseKey(encoded string) (Key, error) {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" {
		return Key{}, ErrNoKey
	}

	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if material, err := enc.DecodeString(trimmed); err == nil {
			return NewKey(material)
		}
	}

	return Key{}, errors.New("secrets: the key is not valid base64")
}

// Encode renders a key for configuration.
func (k Key) Encode() string { return base64.StdEncoding.EncodeToString(k.material) }

// GenerateKey produces a new random master key.
func GenerateKey() (Key, error) {
	material := make([]byte, KeySize)
	if _, err := rand.Read(material); err != nil {
		return Key{}, fmt.Errorf("secrets: generate key: %w", err)
	}

	return NewKey(material)
}

// fingerprint identifies a key without revealing it.
//
// Eight hex characters of SHA-256. It goes into every stored envelope in
// plaintext, so it must say nothing useful about the key: a preimage of
// 32 random bytes from 32 bits of its digest is not a thing anybody is doing.
func fingerprint(material []byte) string {
	sum := sha256.Sum256(material)

	return hex.EncodeToString(sum[:4])
}

// Keyring holds the key to encrypt with and every key needed to decrypt.
//
// More than one, because rotation has a middle: after the key changes there are
// values encrypted with the old one, and an instance that cannot read them has
// not rotated its keys -- it has lost them.
type Keyring struct {
	primary Key
	byID    map[string]Key
}

// NewKeyring builds a keyring. The first key is the one used for encryption;
// the rest are accepted for decryption.
func NewKeyring(primary Key, previous ...Key) (*Keyring, error) {
	if primary.aead == nil {
		return nil, ErrNoKey
	}

	ring := &Keyring{primary: primary, byID: map[string]Key{primary.ID: primary}}

	for _, key := range previous {
		if key.aead == nil {
			return nil, ErrNoKey
		}

		ring.byID[key.ID] = key
	}

	return ring, nil
}

// PrimaryID is the fingerprint of the key new values are encrypted with.
func (r *Keyring) PrimaryID() string { return r.primary.ID }

// Encrypt wraps a fresh data key and encrypts plaintext with it.
func (r *Keyring) Encrypt(purpose, plaintext string) (string, error) {
	// An empty secret stays empty. An OIDC provider using PKCE legitimately
	// has none, and storing a 120-character envelope for "" would make
	// "is a secret configured?" a decryption rather than a length check.
	if plaintext == "" {
		return "", nil
	}

	dataKey := make([]byte, KeySize)
	if _, err := rand.Read(dataKey); err != nil {
		return "", fmt.Errorf("secrets: generate data key: %w", err)
	}

	dataAEAD, err := newAEAD(dataKey)
	if err != nil {
		return "", err
	}

	sealed, err := seal(dataAEAD, []byte(plaintext), purpose)
	if err != nil {
		return "", err
	}

	// The wrapped data key is bound to the same purpose, so an envelope's two
	// halves cannot be recombined across purposes either.
	wrapped, err := seal(r.primary.aead, dataKey, purpose)
	if err != nil {
		return "", err
	}

	return strings.Join([]string{
		prefix, version, r.primary.ID,
		base64.RawURLEncoding.EncodeToString(wrapped),
		base64.RawURLEncoding.EncodeToString(sealed),
	}, "."), nil
}

/*
Decrypt reads an envelope.

A value that is not an envelope is returned unchanged, and that is a deliberate
migration affordance rather than an oversight: instances that predate this
package have plaintext secrets in the database, and a reader that refused them
would break every existing SSO login the moment the binary was upgraded.
`pivot secrets rewrap` converts them, and `pivot doctor` reports how many are
left.

It is safe because an envelope is unforgeable in the direction that matters: a
plaintext secret cannot accidentally look like one, and an envelope that has
been tampered with fails authentication rather than falling through to this
path.
*/
func (r *Keyring) Decrypt(purpose, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}

	if !IsEnvelope(stored) {
		return stored, nil
	}

	parts := strings.Split(stored, ".")
	if len(parts) != fields {
		return "", ErrMalformed
	}

	key, ok := r.byID[parts[2]]
	if !ok {
		return "", fmt.Errorf("%w: key %s", ErrUnknownKey, parts[2])
	}

	wrapped, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", ErrMalformed
	}

	sealed, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return "", ErrMalformed
	}

	dataKey, err := open(key.aead, wrapped, purpose)
	if err != nil {
		return "", err
	}

	dataAEAD, err := newAEAD(dataKey)
	if err != nil {
		return "", err
	}

	plaintext, err := open(dataAEAD, sealed, purpose)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// NeedsRewrap reports whether a stored value should be re-encrypted: either it
// is still plaintext, or it names a key that is no longer the primary one.
func (r *Keyring) NeedsRewrap(stored string) bool {
	if stored == "" {
		return false
	}

	if !IsEnvelope(stored) {
		return true
	}

	parts := strings.Split(stored, ".")

	return len(parts) != fields || parts[2] != r.primary.ID
}

// IsEnvelope reports whether a stored value is one of ours.
func IsEnvelope(stored string) bool {
	return strings.HasPrefix(stored, prefix+"."+version+".")
}

// newAEAD builds AES-GCM over a key.
func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: build cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: build gcm: %w", err)
	}

	return aead, nil
}

// seal encrypts with a fresh nonce, returning nonce||ciphertext.
func seal(aead cipher.AEAD, plaintext []byte, purpose string) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: generate nonce: %w", err)
	}

	// The nonce is prepended rather than stored separately: it is not secret,
	// it is meaningless without the ciphertext, and a format where the two can
	// be separated is a format where they eventually are.
	return aead.Seal(nonce, nonce, plaintext, []byte(purpose)), nil
}

// open reverses seal.
func open(aead cipher.AEAD, sealed []byte, purpose string) ([]byte, error) {
	if len(sealed) < aead.NonceSize() {
		return nil, ErrMalformed
	}

	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(purpose))
	if err != nil {
		// Never wrapped: the reason authentication failed -- wrong key, wrong
		// purpose, flipped bit -- is exactly what an attacker probing a
		// decryption oracle wants to learn.
		return nil, ErrMalformed
	}

	return plaintext, nil
}

/*
Refusing is the default.

A Pivot with no key configured must not quietly write plaintext secrets: that
is the failure this package exists to prevent, and it would be invisible until
somebody read the database. So the zero value of the cipher a caller gets
refuses to encrypt, loudly, and still reads existing plaintext so that an
instance which has not been given a key yet keeps working for everything except
storing a new secret.
*/
type refusing struct{}

// Refusing returns a Cipher that cannot encrypt.
func Refusing() Cipher { return refusing{} }

// plaintext passes values through untouched.
//
// Exactly one caller wants this: the tooling that reads a column as the
// database holds it, in order to tell a sealed value from a plaintext one and
// rewrap it. It is a named type rather than a nil cipher so that a reader of
// that code sees a decision, and so that nothing else acquires it by accident.
type plaintext struct{}

// Plaintext returns a Cipher that stores and returns values unchanged.
//
// Not for storing secrets. See the type's comment.
func Plaintext() Cipher { return plaintext{} }

func (plaintext) Encrypt(_, value string) (string, error) { return value, nil }
func (plaintext) Decrypt(_, value string) (string, error) { return value, nil }

func (refusing) Encrypt(_, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	return "", ErrNoKey
}

func (refusing) Decrypt(_, stored string) (string, error) {
	if IsEnvelope(stored) {
		return "", ErrNoKey
	}

	return stored, nil
}
