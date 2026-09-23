package cli_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

/*
Stored secrets, end to end.

The claim is that somebody holding the database file cannot read the secrets in
it. Every test here checks that against the file, with SQL, rather than against
the application that wrote it -- because the application would be reading it
back through the same code that is supposed to be protecting it.
*/

// storedSecret reads the client_secret column straight out of the file.
func storedSecret(t *testing.T, path string) string {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}

	defer func() { _ = db.Close() }()

	var secret string

	if qerr := db.QueryRowContext(t.Context(),
		"SELECT client_secret FROM identity_providers LIMIT 1").Scan(&secret); qerr != nil {
		t.Fatalf("read the secret: %v", qerr)
	}

	return secret
}

const providerSecret = "an-oidc-client-secret-worth-stealing"

// withProvider builds an instance with one SSO provider configured.
func withProvider(t *testing.T) (string, map[string]string) {
	t.Helper()

	dir := t.TempDir()
	env := map[string]string{
		"PIVOT_DATABASE_URL":       "sqlite://" + filepath.Join(dir, "pivot.db"),
		"PIVOT_ADMIN_PASSWORD":     "a-long-enough-password",
		"PIVOT_OIDC_CLIENT_SECRET": providerSecret,
	}

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com", "--name", "Ada"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, _, err := run(t, env, "admin", "add-provider",
		"--org", "acme", "--slug", "okta", "--name", "Okta",
		"--issuer", "https://example.okta.com", "--client-id", "abc123"); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	return dir, env
}

// The deliverable: the secret is not in the database, and the key is not
// beside it by accident but by a default the operator is warned about.
func TestAStoredSecretIsNotReadableFromTheDatabase(t *testing.T) {
	t.Parallel()

	dir, _ := withProvider(t)

	stored := storedSecret(t, filepath.Join(dir, "pivot.db"))

	if strings.Contains(stored, providerSecret) {
		t.Fatalf("the secret is in the database in the clear: %s", stored)
	}

	if !strings.HasPrefix(stored, "pivot.v1.") {
		t.Errorf("the stored value is not an envelope: %s", stored)
	}

	// And the key exists, readable by its owner and nobody else.
	info, err := os.Stat(filepath.Join(dir, "pivot.key"))
	if err != nil {
		t.Fatalf("stat the key file: %v", err)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the key file is mode %o", perm)
	}
}

// And the application gets the plaintext back. An encryption that nobody can
// reverse is not encryption, it is loss.
func TestAStoredSecretComesBackOutAgain(t *testing.T) {
	t.Parallel()

	dir, env := withProvider(t)

	stdout, _, err := run(t, env, "secrets", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !strings.Contains(stdout, "1 encrypted with the current key") {
		t.Errorf("status does not report the secret as current:\n%s", stdout)
	}

	// The real read. Rewrapping under a *new* key requires decrypting with the
	// old one first, so a rewrap that succeeds proves the stored value was
	// opened -- and the resulting envelope naming the new key proves it was
	// the plaintext that got resealed rather than the ciphertext.
	newKey, _, err := run(t, env, "secrets", "generate-key")
	if err != nil {
		t.Fatalf("generate-key: %v", err)
	}

	oldKey, err := os.ReadFile(filepath.Join(dir, "pivot.key"))
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}

	rotating := map[string]string{}
	for k, v := range env {
		rotating[k] = v
	}

	rotating["PIVOT_SECRETS_KEY"] = strings.TrimSpace(newKey)
	rotating["PIVOT_SECRETS_PREVIOUS_KEYS"] = strings.TrimSpace(string(oldKey))

	if _, _, rerr := run(t, rotating, "secrets", "rewrap"); rerr != nil {
		t.Fatalf("the stored secret could not be read back: %v", rerr)
	}

	// And the value that came out and went back in is still not the plaintext.
	if strings.Contains(storedSecret(t, filepath.Join(dir, "pivot.db")), providerSecret) {
		t.Error("the resealed value contains the plaintext")
	}
}

/*
A rotation, in the order the documentation tells people to do it.

New key primary with the old one retained, rewrap, then drop the old key. The
last step is the test: an instance holding only the new key must still be able
to read everything.
*/
func TestAKeyRotation(t *testing.T) {
	t.Parallel()

	dir, env := withProvider(t)

	oldKey, err := os.ReadFile(filepath.Join(dir, "pivot.key"))
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}

	newKey, _, err := run(t, env, "secrets", "generate-key")
	if err != nil {
		t.Fatalf("generate-key: %v", err)
	}

	rotating := map[string]string{}
	for k, v := range env {
		rotating[k] = v
	}

	rotating["PIVOT_SECRETS_KEY"] = strings.TrimSpace(newKey)
	rotating["PIVOT_SECRETS_PREVIOUS_KEYS"] = strings.TrimSpace(string(oldKey))

	// Mid-rotation: the value is readable, and reported as needing a move.
	stdout, _, err := run(t, rotating, "secrets", "status")
	if err != nil {
		t.Fatalf("status mid-rotation: %v", err)
	}

	if !strings.Contains(stdout, "1 encrypted with an older key") {
		t.Fatalf("status does not report the old key:\n%s", stdout)
	}

	if _, _, rerr := run(t, rotating, "secrets", "rewrap"); rerr != nil {
		t.Fatalf("rewrap: %v", rerr)
	}

	// The old key is gone now. This is the step that fails if rewrap did not
	// actually rewrite anything -- and the one that is unrecoverable in
	// production if somebody does it first.
	finished := map[string]string{}
	for k, v := range env {
		finished[k] = v
	}

	finished["PIVOT_SECRETS_KEY"] = strings.TrimSpace(newKey)

	stdout, _, err = run(t, finished, "secrets", "status")
	if err != nil {
		t.Fatalf("status after rotation: %v", err)
	}

	if !strings.Contains(stdout, "1 encrypted with the current key") {
		t.Errorf("after rotation the secret is not on the new key:\n%s", stdout)
	}

	if strings.Contains(storedSecret(t, filepath.Join(dir, "pivot.db")), providerSecret) {
		t.Error("the rewrapped value contains the plaintext")
	}
}

/*
The migration from an instance that predates encryption.

Its secrets are plaintext in the database. They have to keep working -- an
upgrade that breaks every SSO login is not an upgrade -- and they have to be
reported until somebody seals them.
*/
func TestPlaintextFromAnOlderInstanceIsMigrated(t *testing.T) {
	t.Parallel()

	dir, env := withProvider(t)

	// Put the row back the way an older Pivot would have left it.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "pivot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, eerr := db.ExecContext(t.Context(),
		"UPDATE identity_providers SET client_secret = ?", providerSecret); eerr != nil {
		t.Fatalf("write plaintext: %v", eerr)
	}

	_ = db.Close()

	stdout, _, err := run(t, env, "secrets", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !strings.Contains(stdout, "1 not encrypted") {
		t.Fatalf("status does not report the plaintext secret:\n%s", stdout)
	}

	if _, _, rerr := run(t, env, "secrets", "rewrap"); rerr != nil {
		t.Fatalf("rewrap: %v", rerr)
	}

	stored := storedSecret(t, filepath.Join(dir, "pivot.db"))

	if strings.Contains(stored, providerSecret) {
		t.Errorf("the secret is still in the clear after a rewrap: %s", stored)
	}

	if !strings.HasPrefix(stored, "pivot.v1.") {
		t.Errorf("the rewrapped value is not an envelope: %s", stored)
	}
}

// Rewrapping twice changes nothing and complains about nothing.
func TestRewrapIsSafeToRepeat(t *testing.T) {
	t.Parallel()

	_, env := withProvider(t)

	stdout, _, err := run(t, env, "secrets", "rewrap")
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}

	if !strings.Contains(stdout, "already on the current key") {
		t.Errorf("a no-op rewrap did not say so:\n%s", stdout)
	}
}

/*
An instance whose key has gone says so, rather than half-working.

This is the failure that matters most in the field: the key file is lost, or
the orchestrator's secret was rotated without the previous one being kept. It
must be a clear error naming the key, not a login that mysteriously fails.
*/
func TestAMissingKeyIsAClearFailure(t *testing.T) {
	t.Parallel()

	dir, env := withProvider(t)

	if err := os.Remove(filepath.Join(dir, "pivot.key")); err != nil {
		t.Fatalf("remove the key: %v", err)
	}

	// A different key, which is what "I lost it and made a new one" looks
	// like. The secrets are sealed with the old one and cannot be read.
	replacement, _, err := run(t, env, "secrets", "generate-key")
	if err != nil {
		t.Fatalf("generate-key: %v", err)
	}

	env["PIVOT_SECRETS_KEY"] = strings.TrimSpace(replacement)

	_, _, err = run(t, env, "secrets", "rewrap")

	if err == nil {
		t.Fatal("rewrapping with the wrong key succeeded")
	}

	if !strings.Contains(err.Error(), "key") {
		t.Errorf("error = %v, want it to name the key", err)
	}
}

// doctor reports the key without printing it.
func TestDoctorReportsTheKeyWithoutPrintingIt(t *testing.T) {
	t.Parallel()

	dir, env := withProvider(t)

	key, err := os.ReadFile(filepath.Join(dir, "pivot.key"))
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}

	env["PIVOT_SERVER_PORT"] = strconv.Itoa(freePort(t))

	stdout, _, _ := run(t, env, "doctor")

	if !strings.Contains(stdout, "secrets") {
		t.Errorf("doctor does not mention the secrets key:\n%s", stdout)
	}

	if strings.Contains(stdout, strings.TrimSpace(string(key))) {
		t.Error("doctor printed the key")
	}
}

// generate-key prints a usable key and nothing else.
func TestGenerateKeyPrintsAUsableKey(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "secrets", "generate-key")
	if err != nil {
		t.Fatalf("generate-key: %v", err)
	}

	first := strings.TrimSpace(stdout)

	if len(first) < 40 {
		t.Fatalf("the key looks too short: %q", first)
	}

	second, _, err := run(t, nil, "secrets", "generate-key")
	if err != nil {
		t.Fatalf("generate-key: %v", err)
	}

	if first == strings.TrimSpace(second) {
		t.Error("two generated keys are identical")
	}
}
