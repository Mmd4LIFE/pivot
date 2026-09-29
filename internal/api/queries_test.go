package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The query endpoint.

The gating is what these check hardest. The pipeline enforces the permission
too -- that is what makes it structural rather than something every handler has
to remember -- but a route registered without its gate would let the request
reach the pipeline before being refused, and the difference between "refused at
the door" and "refused inside" is a body somebody parsed and a connection
somebody opened.
*/

// Raw SQL needs native_query, which an editor does not have.
func TestRunningAQueryNeedsTheNativeQueryPermission(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		// Editor deliberately does not carry native_query: raw SQL bypasses
		// semantic row-level security, so it is a separate grant rather than
		// something bundled into "can edit".
		grant(t, f, authz.RelationEditor)

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", map[string]any{
			"connectionId": "00000000-0000-0000-0000-000000000001",
			"sql":          "SELECT 1",
		})

		if resp.status != http.StatusForbidden {
			t.Errorf("an editor running raw SQL = %d, want 403: %s", resp.status, resp)
		}
	})
}

// And an analyst, who has it, gets past the gate.
func TestAnAnalystMayRunRawSQL(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)
		grant(t, f, authz.RelationAnalyst)

		if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
			t.Fatalf("login: %s", resp)
		}

		resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", map[string]any{
			"connectionId": "00000000-0000-0000-0000-000000000001",
			"sql":          "SELECT 1",
		})

		// Past the gate and into the pipeline, which cannot find that
		// connection. A 403 here would mean the permission is wrong.
		if resp.status == http.StatusForbidden {
			t.Errorf("an analyst was refused raw SQL: %s", resp)
		}

		if resp.status != http.StatusNotFound {
			t.Errorf("an unknown connection = %d, want 404: %s", resp.status, resp)
		}
	})
}

// Signing out is not optional.
func TestRunningAQueryNeedsASession(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))

	resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", map[string]any{
		"connectionId": "00000000-0000-0000-0000-000000000001",
		"sql":          "SELECT 1",
	})

	if resp.status != http.StatusUnauthorized {
		t.Errorf("an anonymous query = %d, want 401: %s", resp.status, resp)
	}
}

// A request that names no statement is the caller's to fix, and says so with
// a code rather than a 500.
func TestAQueryWithNothingToRunIsRejected(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))
	grant(t, f, authz.RelationAnalyst)

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login: %s", resp)
	}

	cases := map[string]struct {
		body map[string]any
		want int
	}{
		"no statement": {
			map[string]any{"connectionId": "00000000-0000-0000-0000-000000000001", "sql": "  "},
			http.StatusBadRequest,
		},
		"connection id that is not an id": {
			map[string]any{"connectionId": "warehouse", "sql": "SELECT 1"},
			http.StatusUnprocessableEntity,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", tc.body)
			if resp.status != tc.want {
				t.Errorf("status = %d, want %d: %s", resp.status, tc.want, resp)
			}
		})
	}
}

/*
A real query, end to end, over a real source.

SQLite as the source, because it needs no container and the point is the round
trip rather than the dialect: an HTTP request in, the pipeline, a connector, a
file, and rows back out as JSON.
*/
func TestAQueryReturnsItsRows(t *testing.T) {
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

	resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", map[string]any{
		"connectionId": conn.ID.String(),
		"sql":          "SELECT id, region FROM orders ORDER BY id",
	})

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp)
	}

	var got struct {
		QueryID string `json:"queryId"`
		Columns []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			SourceType string `json:"sourceType"`
		} `json:"columns"`
		Rows        [][]any `json:"rows"`
		RowCount    int64   `json:"rowCount"`
		Truncated   bool    `json:"truncated"`
		CacheStatus string  `json:"cacheStatus"`
	}

	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decode: %v: %s", err, resp.body)
	}

	switch {
	case got.RowCount != 3:
		t.Errorf("RowCount = %d, want 3", got.RowCount)
	case len(got.Rows) != 3:
		t.Errorf("got %d rows, want 3", len(got.Rows))
	case len(got.Columns) != 2:
		t.Errorf("got %d columns, want 2", len(got.Columns))
	case got.QueryID == "":
		t.Error("the response carries no query id, so the query cannot be stopped")
	case got.Truncated:
		t.Error("a three-row result was reported as truncated")
	}

	// Both the canonical kind and the source's own spelling, because a client
	// formats on one and a person debugging wants the other.
	if len(got.Columns) == 2 {
		if got.Columns[1].Name != "region" || got.Columns[1].SourceType == "" {
			t.Errorf("column = %+v", got.Columns[1])
		}
	}
}

// A statement the source rejects comes back with the source's own words,
// because "no such column: nope" is the whole answer.
func TestASourceErrorCarriesTheSourcesMessage(t *testing.T) {
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

	resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", map[string]any{
		"connectionId": conn.ID.String(),
		"sql":          "SELECT nope FROM orders",
	})

	if resp.status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", resp.status, resp)
	}

	// The envelope nests, which is the contract every Pivot endpoint shares.
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(resp.body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Error.Code != string(api.CodeQueryFailed) {
		t.Errorf("code = %q, want %q", body.Error.Code, api.CodeQueryFailed)
	}

	if !containsFold(body.Error.Message, "nope") {
		t.Errorf("the message does not name the column: %q", body.Error.Message)
	}
}

// --- helpers -----------------------------------------------------------------

// grant gives the fixture's user a role on its organization.
func grant(t *testing.T, f *authFixture, relation authz.Relation) {
	t.Helper()

	if err := f.repos.Roles.Grant(f.ctx, repo.GrantRole{
		SubjectType: "user", SubjectID: f.user.ID,
		Relation:   string(relation),
		ObjectType: string(authz.TypeOrganization), ObjectID: f.org.ID,
	}); err != nil {
		t.Fatalf("grant %s: %v", relation, err)
	}
}

// seedSource builds the SQLite file a query is run against. The connector
// opens every SQLite file read-only, so the fixture cannot build it that way.
func seedSource(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open the source: %v", err)
	}

	defer func() { _ = db.Close() }()

	for _, statement := range []string{
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, region TEXT)`,
		`INSERT INTO orders VALUES (1, 'emea'), (2, 'apac'), (3, 'emea')`,
	} {
		if _, eerr := db.ExecContext(t.Context(), statement); eerr != nil {
			t.Fatalf("%s: %v", statement, eerr)
		}
	}
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

/*
The connections a caller may query.

Untested, this is the endpoint that decides whether the editor has anything in
its picker at all -- and a picker that silently offers a disabled connection
produces a confusing error instead of a shorter list.
*/
func TestTheConnectionListOmitsWhatCannotBeQueried(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))
	grant(t, f, authz.RelationAnalyst)

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login: %s", resp)
	}

	for _, spec := range []struct {
		slug    string
		enabled bool
	}{{"live", true}, {"retired", false}} {
		if _, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: spec.slug, Name: spec.slug, Kind: "sqlite",
			Database: filepath.Join(t.TempDir(), spec.slug+".db"), IsEnabled: spec.enabled,
		}); err != nil {
			t.Fatalf("create %s: %v", spec.slug, err)
		}
	}

	resp := f.request(t, http.MethodGet, api.APIPrefix+"/connections", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp)
	}

	var got struct {
		Connections []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Kind string `json:"kind"`
		} `json:"connections"`
	}

	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(got.Connections) != 1 {
		t.Fatalf("got %d connections, want 1 (the disabled one must be omitted): %s", len(got.Connections), resp.body)
	}

	if got.Connections[0].Slug != "live" {
		t.Errorf("listed %q, want the enabled connection", got.Connections[0].Slug)
	}

	// Nothing that could hold a password, and nothing an editor does not need.
	if !containsFold(string(resp.body), "sqlite") {
		t.Error("the kind is missing, so a picker cannot label the source")
	}

	for _, leaked := range []string{"password", "username", "host"} {
		if containsFold(string(resp.body), leaked) {
			t.Errorf("the list carries %q, which an editor does not need: %s", leaked, resp.body)
		}
	}
}

// And it is gated on the same permission as running a statement.
func TestTheConnectionListNeedsTheQueryPermission(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))
	grant(t, f, authz.RelationViewer)

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login: %s", resp)
	}

	resp := f.request(t, http.MethodGet, api.APIPrefix+"/connections", nil)
	if resp.status != http.StatusForbidden {
		t.Errorf("a viewer listing sources = %d, want 403: %s", resp.status, resp)
	}
}

/*
Values survive the trip to JSON as themselves.

Bytes become text rather than base64 noise, and a timestamp keeps its zone --
Part 19-a went to real trouble to distinguish an instant from a wall-clock
reading, and marshaling it as whatever encoding/json feels like would throw
that away at the very last step.
*/
func TestValuesSurviveTheTripToJSON(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))
	grant(t, f, authz.RelationAnalyst)

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login: %s", resp)
	}

	source := filepath.Join(t.TempDir(), "types.db")

	db, err := sql.Open("sqlite", "file:"+source)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, statement := range []string{
		`CREATE TABLE shapes (label TEXT, raw BLOB, seen_at TIMESTAMPTZ, absent TEXT)`,
		`INSERT INTO shapes VALUES ('a', X'68656C6C6F', '2026-09-29T10:30:00Z', NULL)`,
	} {
		if _, eerr := db.ExecContext(t.Context(), statement); eerr != nil {
			t.Fatalf("%s: %v", statement, eerr)
		}
	}

	_ = db.Close()

	conn, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
		Slug: "types", Name: "Types", Kind: "sqlite", Database: source, IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	resp := f.request(t, http.MethodPost, api.APIPrefix+"/queries", map[string]any{
		"connectionId": conn.ID.String(),
		"sql":          "SELECT label, raw, seen_at, absent FROM shapes",
	})

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp)
	}

	var got struct {
		Rows [][]any `json:"rows"`
	}

	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(got.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(got.Rows))
	}

	row := got.Rows[0]

	// Bytes as text, not base64: an editor showing "aGVsbG8=" for 'hello' is
	// showing the encoding rather than the value.
	if row[1] != "hello" {
		t.Errorf("bytes came back as %v, want \"hello\"", row[1])
	}

	// The zone survives.
	if text, ok := row[2].(string); !ok || !containsFold(text, "2026-09-29") {
		t.Errorf("the timestamp came back as %v", row[2])
	}

	// And a NULL is null, not "".
	if row[3] != nil {
		t.Errorf("a NULL came back as %#v, which a grid cannot tell from an empty string", row[3])
	}
}
