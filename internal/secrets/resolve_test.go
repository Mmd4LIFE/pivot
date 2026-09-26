package secrets_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/secrets"
)

/*
Where the key comes from.

The happy path is one line. Everything else here is a way an operator loses
access to their own secrets: a key file that is not a key, a configured key
that is not base64, a rotation that dropped the old key too early, two processes
starting at once and disagreeing about which key is the key.
*/

func TestAConfiguredKeyIsUsed(t *testing.T) {
	t.Parallel()

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	resolved, err := secrets.Resolve(secrets.Options{Key: key.Encode()}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.Source != secrets.SourceConfig {
		t.Errorf("source = %s, want %s", resolved.Source, secrets.SourceConfig)
	}

	if resolved.Keyring.PrimaryID() != key.ID {
		t.Errorf("resolved a different key")
	}
}

/*
A configured key wins over a key file.

Somebody who has explicitly handed this process a key means it. The opposite
precedence would mean a stale file on disk quietly overriding what the
orchestrator just delivered, which is the kind of thing that is only discovered
during an incident.
*/
func TestAConfiguredKeyWinsOverTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pivot.key")

	fileKey, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if werr := os.WriteFile(path, []byte(fileKey.Encode()), 0o600); werr != nil {
		t.Fatalf("write: %v", werr)
	}

	configured, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	resolved, err := secrets.Resolve(
		secrets.Options{Key: configured.Encode(), File: path}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.Keyring.PrimaryID() != configured.ID {
		t.Errorf("the file key won over the configured one")
	}
}

func TestAKeyIsReadFromItsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pivot.key")

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// With a trailing newline, because that is what writing a key with a shell
	// produces and refusing it would be a cruel way to spend an afternoon.
	if werr := os.WriteFile(path, []byte(key.Encode()+"\n"), 0o600); werr != nil {
		t.Fatalf("write: %v", werr)
	}

	resolved, err := secrets.Resolve(secrets.Options{File: path}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.Source != secrets.SourceFile || resolved.Path != path {
		t.Errorf("source = %s at %q", resolved.Source, resolved.Path)
	}

	if resolved.Keyring.PrimaryID() != key.ID {
		t.Error("read a different key than was written")
	}
}

// The first run: no key anywhere, so one is made and kept.
func TestAMissingKeyIsGeneratedAndReused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pivot.key")

	first, err := secrets.Resolve(secrets.Options{File: path, Generate: true}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if first.Source != secrets.SourceGenerated {
		t.Errorf("source = %s, want %s", first.Source, secrets.SourceGenerated)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the key file is mode %o", perm)
	}

	// The second start reads what the first wrote. A generator that ran twice
	// would make every secret written in between unreadable.
	second, err := secrets.Resolve(secrets.Options{File: path, Generate: true}, nil)
	if err != nil {
		t.Fatalf("resolve again: %v", err)
	}

	if second.Source != secrets.SourceFile {
		t.Errorf("the second start generated another key (source %s)", second.Source)
	}

	if second.Keyring.PrimaryID() != first.Keyring.PrimaryID() {
		t.Error("the second start resolved a different key")
	}
}

/*
Two processes starting at once must not disagree about the key.

Whichever loses the race reads the winner's file rather than overwriting it.
An overwrite here is the worst failure this package has: every secret written
between the two starts becomes unreadable, and nothing reports it until
somebody tries to log in.
*/
func TestTwoSimultaneousStartsAgreeOnOneKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pivot.key")

	const starts = 8

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		ids = map[string]int{}
	)

	start := make(chan struct{})

	for range starts {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			resolved, err := secrets.Resolve(secrets.Options{File: path, Generate: true}, nil)
			if err != nil {
				return
			}

			mu.Lock()
			ids[resolved.Keyring.PrimaryID()]++
			mu.Unlock()
		}()
	}

	close(start)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(ids) != 1 {
		t.Fatalf("%d starts produced %d different keys: %v", starts, len(ids), ids)
	}

	if total := ids[keyOf(ids)]; total != starts {
		t.Errorf("only %d of %d starts resolved a key", total, starts)
	}
}

func keyOf(m map[string]int) string {
	for k := range m {
		return k
	}

	return ""
}

// Generation is opt-in, so a command that only reads cannot create a key. A
// diagnostic that changed the state it was diagnosing would be worse than none.
func TestAMissingKeyIsNotGeneratedUnlessAsked(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pivot.key")

	_, err := secrets.Resolve(secrets.Options{File: path}, nil)

	if !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("error = %v, want ErrNoKey", err)
	}

	if _, serr := os.Stat(path); !errors.Is(serr, os.ErrNotExist) {
		t.Error("a read-only resolve created a key file")
	}
}

/*
A key file that is not a key.

A truncated write, or the wrong file entirely. The error must not suggest
deleting it: deleting it is what turns "this looks wrong" into "the secrets are
gone", and the file is far more often recoverable than the secrets are.
*/
func TestAnUnreadableKeyFileSaysNotToDeleteIt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pivot.key")

	if err := os.WriteFile(path, []byte("this is not a key"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := secrets.Resolve(secrets.Options{File: path, Generate: true}, nil)

	if err == nil {
		t.Fatal("a key file full of rubbish was accepted")
	}

	if !strings.Contains(err.Error(), "Do not delete it") {
		t.Errorf("error = %v, want it to warn against deleting the file", err)
	}

	// And it was not replaced, even though generating was allowed.
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}

	if string(data) != "this is not a key" {
		t.Error("the unreadable key file was overwritten")
	}
}

func TestABadConfiguredKeyIsRefused(t *testing.T) {
	t.Parallel()

	_, err := secrets.Resolve(secrets.Options{Key: "not-a-key"}, nil)

	if err == nil {
		t.Fatal("a configured key that is not base64 was accepted")
	}

	if !strings.Contains(err.Error(), "configured key") {
		t.Errorf("error = %v, want it to name the configured key", err)
	}
}

// Previous keys are loaded for reading, and a bad one is named by position so
// somebody with three of them knows which to look at.
func TestPreviousKeysAreLoadedAndValidated(t *testing.T) {
	t.Parallel()

	primary, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	older, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	sealedBefore, err := mustRing(t, older).Encrypt(purpose, "an-old-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	resolved, err := secrets.Resolve(secrets.Options{
		Key: primary.Encode(), PreviousKeys: []string{older.Encode()},
	}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, err := resolved.Keyring.Decrypt(purpose, sealedBefore)
	if err != nil {
		t.Fatalf("decrypt with the retained key: %v", err)
	}

	if got != "an-old-secret" {
		t.Errorf("decrypted %q", got)
	}

	_, err = secrets.Resolve(secrets.Options{
		Key: primary.Encode(), PreviousKeys: []string{older.Encode(), "rubbish"},
	}, nil)

	if err == nil {
		t.Fatal("a previous key that is not base64 was accepted")
	}

	if !strings.Contains(err.Error(), "previous key 2") {
		t.Errorf("error = %v, want it to name which previous key", err)
	}
}

// With neither a key nor a file there is nothing to resolve, and saying so is
// better than defaulting to a location nobody asked for.
func TestNoKeyAndNoFileIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := secrets.Resolve(secrets.Options{}, nil); err == nil {
		t.Fatal("resolving with nothing configured succeeded")
	}
}

// The key file sits beside the database, which is the only location a
// zero-config instance knows about.
func TestTheDefaultKeyFileSitsBesideTheDatabase(t *testing.T) {
	t.Parallel()

	for database, want := range map[string]string{
		"/var/lib/pivot/pivot.db": "/var/lib/pivot/" + secrets.DefaultKeyFileName,
		"data/pivot.db":           "data/" + secrets.DefaultKeyFileName,
		"pivot.db":                secrets.DefaultKeyFileName,
		"":                        secrets.DefaultKeyFileName,
	} {
		if got := secrets.DefaultKeyFile(database); got != want {
			t.Errorf("DefaultKeyFile(%q) = %q, want %q", database, got, want)
		}
	}
}

func mustRing(t *testing.T, key secrets.Key) *secrets.Keyring {
	t.Helper()

	ring, err := secrets.NewKeyring(key)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	return ring
}

/*
The empty-file window, deterministically.

The concurrency test above hits this by luck. This one arranges it: a file that
exists and is empty, which is exactly what a sibling process looks like between
`O_EXCL` creating the file and the key being written into it. Resolve must wait
for the contents rather than declare the file unusable.

This is the bug the race test found, pinned so it cannot come back quietly.
*/
func TestAnEmptyKeyFileIsWaitedForRatherThanCondemned(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pivot.key")

	// The file exists and holds nothing, as it does for a few microseconds in
	// production and for as long as this test likes here.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The sibling finishes its write shortly after we start looking.
	go func() {
		time.Sleep(80 * time.Millisecond)

		_ = os.WriteFile(path, []byte(key.Encode()+"\n"), 0o600)
	}()

	resolved, err := secrets.Resolve(secrets.Options{File: path, Generate: true}, nil)
	if err != nil {
		t.Fatalf("resolve while the key was being written: %v", err)
	}

	if resolved.Keyring.PrimaryID() != key.ID {
		t.Error("resolved a different key than the one that was written")
	}
}

// And the wait is bounded. A file that stays empty really is unusable, and
// saying so eventually is better than hanging at startup forever.
func TestAnEmptyKeyFileEventuallyGivesUp(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pivot.key")

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	start := time.Now()

	_, err := secrets.Resolve(secrets.Options{File: path, Generate: true}, nil)

	if err == nil {
		t.Fatal("a permanently empty key file was accepted")
	}

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("gave up after %s, which is long enough to look like a hang", elapsed)
	}

	if !strings.Contains(err.Error(), "Do not delete it") {
		t.Errorf("error = %v, want the ordinary unusable-key message", err)
	}
}

// Previous keys survive the wait. The loser of a creation race is still in the
// middle of a rotation, and dropping its retained keys would make the values it
// then reads unopenable.
func TestPreviousKeysSurviveTheWait(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pivot.key")

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	older, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	sealed, err := mustRing(t, older).Encrypt(purpose, "an-old-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	newer, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	go func() {
		time.Sleep(80 * time.Millisecond)

		_ = os.WriteFile(path, []byte(newer.Encode()+"\n"), 0o600)
	}()

	resolved, err := secrets.Resolve(secrets.Options{
		File: path, Generate: true, PreviousKeys: []string{older.Encode()},
	}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if _, derr := resolved.Keyring.Decrypt(purpose, sealed); derr != nil {
		t.Errorf("the retained key was dropped during the wait: %v", derr)
	}
}

// A key file that is a directory, or lives under one that cannot be created.
// Both are configuration mistakes, and both must say which path was the
// problem rather than failing somewhere later.
func TestAnUnusableKeyPathIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	t.Run("the path is a directory", func(t *testing.T) {
		t.Parallel()

		asDir := filepath.Join(dir, "a-directory")
		if err := os.Mkdir(asDir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if _, err := secrets.Resolve(
			secrets.Options{File: asDir, Generate: true}, nil); err == nil {
			t.Error("a directory was accepted as a key file")
		}
	})

	t.Run("the parent is a file", func(t *testing.T) {
		t.Parallel()

		asFile := filepath.Join(dir, "a-file")
		if err := os.WriteFile(asFile, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		_, err := secrets.Resolve(
			secrets.Options{File: filepath.Join(asFile, "pivot.key"), Generate: true}, nil)

		if err == nil {
			t.Error("a key path under a regular file was accepted")
		}
	})
}

// A directory nothing can be written to. The generated-key path has to fail
// here rather than reporting a key it did not manage to keep.
func TestGeneratingIntoAnUnwritableDirectoryFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write to anything")
	}

	t.Parallel()

	dir := t.TempDir()

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := secrets.Resolve(
		secrets.Options{File: filepath.Join(dir, "pivot.key"), Generate: true}, nil); err == nil {
		t.Error("a key was reported as generated in a directory that cannot be written to")
	}
}
