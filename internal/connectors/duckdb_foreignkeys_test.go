//go:build duckdb

package connectors_test

import (
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

// DuckDB's composite keys, paired the same way. Its catalog hands back two
// parallel lists, so this is the same trap as PostgreSQL's in a different
// dialect.
func TestDuckDBPairsACompositeForeignKey(t *testing.T) {
	c := open(t, connectors.Config{
		Kind: connectors.KindDuckDB, Database: connectors.InMemory,
	})

	for _, statement := range []string{fkParentDDL, fkChildDDL} {
		if _, err := c.Query(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	keys, err := c.ForeignKeys(t.Context())
	if err != nil {
		t.Fatalf("ForeignKeys: %v", err)
	}

	var ours []connectors.ForeignKey

	for _, key := range keys {
		if key.FromTable == "fk_child" {
			ours = append(ours, key)
		}
	}

	if len(ours) != 2 {
		t.Fatalf("a two-column key came back as %d rows, want 2:\n%+v", len(ours), ours)
	}

	for i, want := range []string{"tenant_id", "code"} {
		if ours[i].FromColumn != want || ours[i].ToColumn != want {
			t.Errorf("row %d pairs %s -> %s, want %s -> %s",
				i, ours[i].FromColumn, ours[i].ToColumn, want, want)
		}

		if ours[i].Ordinal != i+1 {
			t.Errorf("row %d has ordinal %d, want %d", i, ours[i].Ordinal, i+1)
		}
	}
}
