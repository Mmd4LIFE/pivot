package secrets_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/secrets"
)

/*
Envelope encryption.

Most of these tests are about what must *not* work: a ciphertext moved to
another column, a key that is not this instance's, a bit flipped in transit.
A round trip proves the thing works; the rest prove it is worth having.
*/

const purpose = "identity_provider.client_secret"

func newRing(t *testing.T, previous ...secrets.Key) *secrets.Keyring {
	t.Helper()

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	ring, err := secrets.NewKeyring(key, previous...)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	return ring
}

func TestARoundTrip(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	const plaintext = "an-oidc-client-secret"

	sealed, err := ring.Encrypt(purpose, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if strings.Contains(sealed, plaintext) {
		t.Fatalf("the plaintext is visible in the envelope: %s", sealed)
	}

	got, err := ring.Decrypt(purpose, sealed)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if got != plaintext {
		t.Errorf("round trip = %q, want %q", got, plaintext)
	}
}

// The envelope says what it is, which version it is, and which key it needs.
// An operator looking at a row should be able to tell an encrypted secret from
// a plaintext one without asking anybody.
func TestTheEnvelopeIsSelfDescribing(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	sealed, err := ring.Encrypt(purpose, "a-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	parts := strings.Split(sealed, ".")

	if len(parts) != 5 {
		t.Fatalf("envelope has %d fields, want 5: %s", len(parts), sealed)
	}

	if parts[0] != "pivot" || parts[1] != "v1" {
		t.Errorf("envelope starts %q.%q, want pivot.v1", parts[0], parts[1])
	}

	if parts[2] != ring.PrimaryID() {
		t.Errorf("envelope names key %q, want %q", parts[2], ring.PrimaryID())
	}

	if !secrets.IsEnvelope(sealed) {
		t.Error("IsEnvelope does not recognize an envelope it produced")
	}

	if secrets.IsEnvelope("an-ordinary-plaintext-secret") {
		t.Error("IsEnvelope claims a plaintext secret is an envelope")
	}
}

// Every encryption of the same value differs. Identical ciphertexts would tell
// anybody with read access which providers share a secret.
func TestTheSameSecretEncryptsDifferentlyEveryTime(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	first, err := ring.Encrypt(purpose, "the-same-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	second, err := ring.Encrypt(purpose, "the-same-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if first == second {
		t.Error("two encryptions of the same value are identical")
	}
}

/*
A ciphertext cannot be moved.

Without the purpose bound into the encryption, somebody with write access to
the database could copy a client secret into another column, or another row's,
and have it decrypt happily wherever it landed. The purpose is authenticated,
not secret, which is exactly what AEAD's additional data is for.
*/
func TestACiphertextCannotBeMovedToAnotherColumn(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	sealed, err := ring.Encrypt("identity_provider.client_secret", "a-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = ring.Decrypt("connection.password", sealed)

	if !errors.Is(err, secrets.ErrMalformed) {
		t.Fatalf("decrypting under another purpose = %v, want it refused", err)
	}
}

// A tampered envelope fails rather than returning something.
func TestTamperingIsDetected(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	sealed, err := ring.Encrypt(purpose, "a-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	for name, mutated := range map[string]string{
		"ciphertext": sealed[:len(sealed)-1] + flip(sealed[len(sealed)-1:]),
		"wrapped key": strings.Join(append(
			strings.Split(sealed, ".")[:3],
			flip(strings.Split(sealed, ".")[3][:1])+strings.Split(sealed, ".")[3][1:],
			strings.Split(sealed, ".")[4]), "."),
		"truncated": sealed[:len(sealed)-8],
		"no fields": "pivot.v1.deadbeef",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, derr := ring.Decrypt(purpose, mutated); derr == nil {
				t.Error("a tampered envelope decrypted successfully")
			}
		})
	}
}

/*
The middle of a rotation.

After the key changes there are values encrypted with the old one. An instance
that cannot read them has not rotated its keys, it has lost them -- so the
keyring keeps every key it is given for reading, and encrypts only with the
first.
*/
func TestOldKeysStillDecryptAfterRotation(t *testing.T) {
	t.Parallel()

	oldKey, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	before, err := secrets.NewKeyring(oldKey)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	sealed, err := before.Encrypt(purpose, "the-old-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// The rotated instance: a new primary, the old key retained.
	after := newRing(t, oldKey)

	got, err := after.Decrypt(purpose, sealed)
	if err != nil {
		t.Fatalf("decrypt an old value after rotation: %v", err)
	}

	if got != "the-old-secret" {
		t.Errorf("decrypted %q", got)
	}

	// And it is reported as needing rewrapping, because it is still protected
	// by the key that was rotated away from.
	if !after.NeedsRewrap(sealed) {
		t.Error("a value under the old key does not report as needing a rewrap")
	}

	rewrapped, err := after.Encrypt(purpose, got)
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}

	if after.NeedsRewrap(rewrapped) {
		t.Error("a freshly written value reports as needing a rewrap")
	}
}

// A key this instance does not have is a distinct, recoverable failure: find
// the key. Reporting it as corruption would send somebody to restore a backup
// they do not need.
func TestAnUnknownKeyIsDistinctFromCorruption(t *testing.T) {
	t.Parallel()

	stranger := newRing(t)

	sealed, err := stranger.Encrypt(purpose, "a-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = newRing(t).Decrypt(purpose, sealed)

	if !errors.Is(err, secrets.ErrUnknownKey) {
		t.Fatalf("error = %v, want ErrUnknownKey", err)
	}
}

/*
Plaintext reads through unchanged.

Instances that predate this package have plaintext secrets in the database, and
a reader that refused them would break every existing SSO login the moment the
binary was upgraded. They are reported as needing a rewrap instead.
*/
func TestPlaintextIsReadableAndReportedAsNeedingARewrap(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	const legacy = "a-secret-stored-before-any-of-this-existed"

	got, err := ring.Decrypt(purpose, legacy)
	if err != nil {
		t.Fatalf("decrypt legacy plaintext: %v", err)
	}

	if got != legacy {
		t.Errorf("legacy plaintext = %q, want it unchanged", got)
	}

	if !ring.NeedsRewrap(legacy) {
		t.Error("plaintext does not report as needing a rewrap")
	}
}

// An empty secret stays empty. An OIDC provider using PKCE legitimately has
// none, and an envelope around nothing would make "is one configured?" a
// decryption rather than a length check.
func TestAnEmptySecretStaysEmpty(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	sealed, err := ring.Encrypt(purpose, "")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if sealed != "" {
		t.Errorf("an empty secret encrypted to %q", sealed)
	}

	if ring.NeedsRewrap("") {
		t.Error("an empty secret reports as needing a rewrap")
	}
}

/*
With no key, writing a secret fails and reading an old one works.

The failure this package exists to prevent is an instance quietly storing
plaintext because nobody configured a key. Refusing the write is loud and
recoverable; writing plaintext is silent and is only discovered by somebody
reading the database.
*/
func TestWithoutAKeyWritingIsRefusedAndReadingPlaintextIsNot(t *testing.T) {
	t.Parallel()

	c := secrets.Refusing()

	if _, err := c.Encrypt(purpose, "a-secret"); !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("encrypt without a key = %v, want ErrNoKey", err)
	}

	got, err := c.Decrypt(purpose, "an-old-plaintext-secret")
	if err != nil {
		t.Fatalf("read plaintext without a key: %v", err)
	}

	if got != "an-old-plaintext-secret" {
		t.Errorf("plaintext = %q", got)
	}

	// But an envelope it cannot open is an error rather than a string that
	// happens to start with "pivot.v1".
	if _, err := c.Decrypt(purpose, "pivot.v1.deadbeef.AAAA.AAAA"); !errors.Is(err, secrets.ErrNoKey) {
		t.Errorf("decrypting an envelope without a key = %v, want ErrNoKey", err)
	}
}

// A key's identity comes from its material, so two instances given the same
// key agree on which key it is without being told.
func TestAKeyIdentifiesItself(t *testing.T) {
	t.Parallel()

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	same, err := secrets.ParseKey(key.Encode())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if same.ID != key.ID {
		t.Errorf("the same key round-tripped to a different ID: %s vs %s", same.ID, key.ID)
	}

	other, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if other.ID == key.ID {
		t.Error("two different keys share an ID")
	}

	// And the fingerprint is not the key.
	if strings.Contains(key.Encode(), key.ID) {
		t.Error("the fingerprint appears in the encoded key")
	}
}

// Keys arrive through shells, YAML and secret managers, and come out in
// whichever base64 flavor the last one preferred.
func TestAKeyIsAcceptedInEveryBase64Flavour(t *testing.T) {
	t.Parallel()

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	padded := key.Encode()
	unpadded := strings.TrimRight(padded, "=")
	urlSafe := strings.NewReplacer("+", "-", "/", "_").Replace(padded)

	for name, encoded := range map[string]string{
		"standard":        padded,
		"unpadded":        unpadded,
		"url safe":        urlSafe,
		"with whitespace": "  " + padded + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parsed, perr := secrets.ParseKey(encoded)
			if perr != nil {
				t.Fatalf("parse %s: %v", name, perr)
			}

			if parsed.ID != key.ID {
				t.Errorf("%s parsed to a different key", name)
			}
		})
	}
}

func TestABadKeyIsRefused(t *testing.T) {
	t.Parallel()

	for name, encoded := range map[string]string{
		"empty":      "",
		"not base64": "!!!!not base64!!!!",
		"too short":  "c2hvcnQ=",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := secrets.ParseKey(encoded); err == nil {
				t.Errorf("%s was accepted as a key", name)
			}
		})
	}
}

// flip changes one character, for the tampering tests.
func flip(s string) string {
	if s == "" {
		return "A"
	}

	if s[0] == 'A' {
		return "B" + s[1:]
	}

	return "A" + s[1:]
}

// A keyring cannot be built from a key that was never initialized. The zero
// value of a Key looks usable and holds no cipher, so this is the difference
// between an error at startup and a panic on the first secret.
func TestAKeyringRefusesAZeroKey(t *testing.T) {
	t.Parallel()

	if _, err := secrets.NewKeyring(secrets.Key{}); !errors.Is(err, secrets.ErrNoKey) {
		t.Errorf("a zero primary key = %v, want ErrNoKey", err)
	}

	usable, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, err := secrets.NewKeyring(usable, secrets.Key{}); !errors.Is(err, secrets.ErrNoKey) {
		t.Errorf("a zero previous key = %v, want ErrNoKey", err)
	}
}

/*
An envelope whose fields are not what they claim.

The shape is right -- five fields, the correct prefix, a key this instance has
-- and the contents are rubbish. Somebody editing the database by hand, or a
column that has been through a lossy export. Each has to fail rather than
produce a string.
*/
func TestAnEnvelopeWithUnreadableFieldsIsRefused(t *testing.T) {
	t.Parallel()

	ring := newRing(t)

	sealed, err := ring.Encrypt(purpose, "a-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	parts := strings.Split(sealed, ".")

	for name, mutated := range map[string]string{
		"the wrapped key is not base64": strings.Join(
			[]string{parts[0], parts[1], parts[2], "not base64!!", parts[4]}, "."),
		"the ciphertext is not base64": strings.Join(
			[]string{parts[0], parts[1], parts[2], parts[3], "not base64!!"}, "."),
		"the ciphertext is shorter than a nonce": strings.Join(
			[]string{parts[0], parts[1], parts[2], parts[3], "AAAA"}, "."),
		"the wrapped key is shorter than a nonce": strings.Join(
			[]string{parts[0], parts[1], parts[2], "AAAA", parts[4]}, "."),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, derr := ring.Decrypt(purpose, mutated); !errors.Is(derr, secrets.ErrMalformed) {
				t.Errorf("error = %v, want ErrMalformed", derr)
			}
		})
	}
}

/*
The plaintext cipher does nothing, in both directions.

It exists for one caller: the tooling that reads a column as the database holds
it, in order to tell a sealed value from a plaintext one. A test, because "does
nothing" is a contract like any other and the day it starts doing something is
the day secrets get written in the clear.
*/
func TestThePlaintextCipherIsATruePassThrough(t *testing.T) {
	t.Parallel()

	c := secrets.Plaintext()

	ring := newRing(t)

	sealed, err := ring.Encrypt(purpose, "a-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	for name, value := range map[string]string{
		"a plaintext secret": "an-ordinary-secret",
		"an envelope":        sealed,
		"nothing":            "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, eerr := c.Encrypt(purpose, value)
			if eerr != nil || out != value {
				t.Errorf("Encrypt(%s) = %q, %v; want it unchanged", name, out, eerr)
			}

			back, derr := c.Decrypt(purpose, value)
			if derr != nil || back != value {
				t.Errorf("Decrypt(%s) = %q, %v; want it unchanged", name, back, derr)
			}
		})
	}
}
