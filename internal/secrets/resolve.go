package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultKeyFileName is the key file's name when none is configured.
const DefaultKeyFileName = "pivot.key"

// Source says where the key came from, for the startup banner and for
// `pivot doctor`.
type Source string

const (
	// SourceConfig means it was supplied directly, usually by an orchestrator.
	SourceConfig Source = "configuration"

	// SourceFile means it was read from a file.
	SourceFile Source = "key file"

	// SourceGenerated means there was none and one was created.
	SourceGenerated Source = "generated"
)

// Resolved is a keyring and where it came from.
type Resolved struct {
	Keyring *Keyring
	Source  Source

	// Path is the key file, when there is one. Empty for a configured key.
	Path string
}

// Options describe where to look for keys.
type Options struct {
	// Key, base64-encoded. Wins over File when both are present: an operator
	// who has explicitly handed this process a key means it.
	Key string

	// File to read from, and to write to when generating.
	File string

	// PreviousKeys are accepted for decryption only.
	PreviousKeys []string

	// Generate allows creating a key when none exists. Commands that only
	// read -- `doctor`, `config show` -- leave it false, so that diagnosing an
	// instance cannot change it.
	Generate bool
}

/*
Resolve assembles the keyring.

Order: the configured key, then the key file, then -- if allowed -- a new key
written to that file. The last case is what makes the zero-config first run
encrypt anything at all, and it is a deliberate compromise: a key beside the
database protects a stolen database, a stolen backup and a decommissioned disk,
and does not protect a stolen machine. Refusing to start without an
operator-provisioned key would protect against more and would mean nobody gets
past the first run.
*/
func Resolve(opts Options, log *slog.Logger) (*Resolved, error) {
	previous, err := parseAll(opts.PreviousKeys)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(opts.Key) != "" {
		key, perr := ParseKey(opts.Key)
		if perr != nil {
			return nil, fmt.Errorf("the configured key: %w", perr)
		}

		ring, rerr := NewKeyring(key, previous...)
		if rerr != nil {
			return nil, rerr
		}

		return &Resolved{Keyring: ring, Source: SourceConfig}, nil
	}

	path := opts.File
	if path == "" {
		return nil, errors.New("secrets: no key and no key file to read one from")
	}

	material, err := os.ReadFile(filepath.Clean(path))

	switch {
	case err == nil:
		// An empty file is not a key and never will be -- but it is exactly
		// what a sibling process that has just won the creation race looks
		// like, because O_EXCL makes the file appear before its contents do.
		// When this process was willing to create one, it waits instead of
		// declaring the other one's key unusable.
		if strings.TrimSpace(string(material)) == "" && opts.Generate {
			return awaitKey(path, previous, log)
		}

		key, perr := ParseKey(string(material))
		if perr != nil {
			// Never "the key is wrong": a key file that exists and cannot be
			// read is far more often a truncated write or the wrong file than
			// a corrupt key, and deleting it to start again destroys every
			// secret in the database.
			return nil, fmt.Errorf(
				"%s exists but is not a usable key (%w). Do not delete it: "+
					"without it the stored secrets cannot be read", path, perr)
		}

		ring, rerr := NewKeyring(key, previous...)
		if rerr != nil {
			return nil, rerr
		}

		return &Resolved{Keyring: ring, Source: SourceFile, Path: path}, nil

	case errors.Is(err, fs.ErrNotExist):
		if !opts.Generate {
			return nil, fmt.Errorf("%w: %s does not exist", ErrNoKey, path)
		}

		return generate(path, previous, log)

	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
}

// generate creates a key file.
func generate(path string, previous []Key, log *slog.Logger) (*Resolved, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), mkErr)
	}

	// O_EXCL, so two processes starting at once cannot both decide they are
	// the one creating the key -- the loser would overwrite the winner's key
	// and every secret written in between would become unreadable.
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Somebody else won. Read theirs rather than failing.
			return awaitKey(path, previous, log)
		}

		return nil, fmt.Errorf("create %s: %w", path, err)
	}

	if _, werr := f.WriteString(key.Encode() + "\n"); werr != nil {
		_ = f.Close()

		return nil, fmt.Errorf("write %s: %w", path, werr)
	}

	if cerr := f.Close(); cerr != nil {
		return nil, fmt.Errorf("write %s: %w", path, cerr)
	}

	ring, err := NewKeyring(key, previous...)
	if err != nil {
		return nil, err
	}

	if log != nil {
		// Warn, not Info. This happens once in an instance's life and the
		// operator has a job to do afterwards: put the file somewhere the
		// database's backups do not go.
		log.Warn("generated an encryption key for stored secrets",
			slog.String("path", path),
			slog.String("key_id", key.ID),
			slog.String("hint",
				"back it up separately from the database; without it the stored secrets are lost"))
	}

	return &Resolved{Keyring: ring, Source: SourceGenerated, Path: path}, nil
}

/*
awaitKey reads a key file another process is in the middle of creating.

O_EXCL makes the file appear before its contents do, so the process that lost
the race can find an empty file where a key is about to be. Reading it once and
reporting "this is not a usable key" would be a false alarm of the worst kind:
the message tells an operator not to delete the file, which is right, but the
file is fine and their instance refused to start.

Bounded, and short. The window is one small write by a sibling process; if a
second passes the file really is unusable and the ordinary error is the honest
answer.
*/
func awaitKey(path string, previous []Key, log *slog.Logger) (*Resolved, error) {
	const (
		attempts = 20
		wait     = 50 * time.Millisecond
	)

	opts := Options{File: path, PreviousKeys: encodeAll(previous)}

	var err error

	for range attempts {
		var resolved *Resolved

		resolved, err = Resolve(opts, log)
		if err == nil {
			return resolved, nil
		}

		time.Sleep(wait)
	}

	return nil, err
}

// DefaultKeyFile is where the key lives when nothing says otherwise: beside
// the database, which is the only location a zero-config instance knows about.
func DefaultKeyFile(databasePath string) string {
	if databasePath == "" {
		return DefaultKeyFileName
	}

	return filepath.Join(filepath.Dir(databasePath), DefaultKeyFileName)
}

func parseAll(encoded []string) ([]Key, error) {
	keys := make([]Key, 0, len(encoded))

	for i, raw := range encoded {
		key, err := ParseKey(raw)
		if err != nil {
			return nil, fmt.Errorf("previous key %d: %w", i+1, err)
		}

		keys = append(keys, key)
	}

	return keys, nil
}

func encodeAll(keys []Key) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key.Encode())
	}

	return out
}
