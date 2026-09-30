package api_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The export endpoint.

Same door as /queries, opposite shape: the body is a file that starts before
the last row arrives. These check the gate, the Content-Disposition, and that a
CSV of a real source's rows is what left.
*/

func TestExportingNeedsTheNativeQueryPermission(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)
		grant(t, f, authz.RelationEditor)

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		resp := f.request(t, http.MethodPost, api.APIPrefix+"/exports", map[string]any{
			"connectionId": "00000000-0000-0000-0000-000000000001",
			"sql":          "SELECT 1",
			"format":       "csv",
		})

		if resp.status != http.StatusForbidden {
			t.Errorf("an editor exporting = %d, want 403: %s", resp.status, resp)
		}
	})
}

func TestAnExportStreamsACSV(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))
	grant(t, f, authz.RelationAnalyst)

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login: %s", resp)
	}

	source := filepath.Join(t.TempDir(), "source.db")
	seedSource(t, source)

	conn, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
		Slug: "source", Name: "The source", Kind: "sqlite",
		Database: source, IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	resp := f.request(t, http.MethodPost, api.APIPrefix+"/exports", map[string]any{
		"connectionId": conn.ID.String(),
		"sql":          "SELECT id, region FROM orders ORDER BY id",
		"format":       "csv",
	})

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp)
	}

	if ct := resp.header("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}

	disposition := resp.header("Content-Disposition")
	if !strings.Contains(disposition, "attachment") || !strings.Contains(disposition, "result.csv") {
		t.Errorf("Content-Disposition = %q, want an attachment named result.csv", disposition)
	}

	if cache := resp.header("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cache)
	}

	want := "id,region\r\n1,emea\r\n2,apac\r\n3,emea\r\n"
	if got := string(resp.body); got != want {
		t.Errorf("body =\n%q\nwant\n%q", got, want)
	}
}

func TestAnUnknownExportFormatIsRefused(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))
	grant(t, f, authz.RelationAnalyst)

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login: %s", resp)
	}

	resp := f.request(t, http.MethodPost, api.APIPrefix+"/exports", map[string]any{
		"connectionId": "00000000-0000-0000-0000-000000000001",
		"sql":          "SELECT 1",
		"format":       "xlsx",
	})

	if resp.status != http.StatusUnprocessableEntity {
		t.Errorf("xlsx before 24-b = %d, want 422: %s", resp.status, resp)
	}

	if !strings.Contains(strings.ToLower(resp.String()), "csv") {
		t.Errorf("refusal should name the formats that exist: %s", resp)
	}
}
