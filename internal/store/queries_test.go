package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// queryDirs are the hand-written SQL sources sqlc reads.
var queryDirs = []string{
	"../../internal/store/queries/postgres",
	"../../internal/store/queries/sqlite",
}

// sqlc's SQLite path rewrites queries by byte offset and miscounts when a
// comment contains a multibyte character. The offsets shift, the rewrite eats
// the wrong bytes, and generation fails with errors like:
//
//	edited query syntax is invalid: mismatched input 'SELECid'
//
// The message names neither the file's real problem nor the character, and the
// query it points at is valid SQL. Part 3-b lost time to the same mechanism
// with a literal '?' in a comment; this part lost time to an em dash.
//
// So the rule is that query files are ASCII, and this is what enforces it —
// a named failure here costs a minute, where the sqlc error costs an hour.
func TestQueryFilesAreASCII(t *testing.T) {
	t.Parallel()

	for _, dir := range queryDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}

			path := filepath.Join(dir, entry.Name())

			t.Run(filepath.Base(dir)+"/"+entry.Name(), func(t *testing.T) {
				t.Parallel()

				data, rerr := os.ReadFile(filepath.Clean(path))
				if rerr != nil {
					t.Fatalf("read: %v", rerr)
				}

				for lineNo, line := range strings.Split(string(data), "\n") {
					for offset, r := range line {
						if r < utf8.RuneSelf {
							continue
						}

						t.Errorf("line %d column %d contains %q (U+%04X); "+
							"sqlc miscounts offsets on multibyte characters and "+
							"corrupts generation. Use ASCII.",
							lineNo+1, offset+1, r, r)

						break
					}
				}
			})
		}
	}
}

// Named arguments are avoided in the SQLite files for a related reason: sqlc
// generates numbered placeholders for them and mis-substitutes those too. Every
// SQLite query uses bare positional placeholders, and this keeps it that way.
func TestSQLiteQueriesAvoidNamedArguments(t *testing.T) {
	t.Parallel()

	dir := "../../internal/store/queries/sqlite"

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		data, rerr := os.ReadFile(filepath.Clean(filepath.Join(dir, entry.Name())))
		if rerr != nil {
			t.Fatalf("read %s: %v", entry.Name(), rerr)
		}

		if strings.Contains(string(data), "sqlc.arg") {
			t.Errorf("%s uses sqlc.arg; the SQLite generator mis-substitutes "+
				"numbered placeholders, so use bare positional ones", entry.Name())
		}
	}
}
