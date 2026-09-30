package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/query"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
QueryHandler runs SQL against a connected source.

The pipeline's first caller outside a test. Everything built since Part 20-b --
the executor, the cache, the governor, the monitor -- has been constructed only
by tests, which is a strange place for load-bearing code to sit; this is where
it starts carrying weight.

It holds an [query.Executor] and nothing else that can reach a source. That is
the single-door property from Part 20-b, and it is checked structurally rather
than by inspection: no file under internal/api may name the connector package,
and a test in internal/query fails if one does.
*/
type QueryHandler struct {
	executor *query.Executor
	repos    *repo.Repositories
	log      *slog.Logger
}

// NewQueryHandler builds the query endpoint.
func NewQueryHandler(
	executor *query.Executor, repos *repo.Repositories, log *slog.Logger,
) *QueryHandler {
	return &QueryHandler{executor: executor, repos: repos, log: log}
}

// --- wire types ------------------------------------------------------------

type runQueryRequest struct {
	ConnectionID string `json:"connectionId"`
	SQL          string `json:"sql"`

	// MaxRows caps this result. Zero uses the connection's own limit, which
	// is the setting an administrator chose and the one a client should
	// normally leave alone.
	MaxRows int64 `json:"maxRows,omitempty"`
}

type queryColumn struct {
	Name string `json:"name"`

	// Type is the canonical kind, and SourceType is what the source called it.
	// Both, because a client formats on the first and a person debugging wants
	// the second -- Part 19-a kept the source spelling for exactly this.
	Type       string `json:"type"`
	SourceType string `json:"sourceType"`
}

type runQueryResponse struct {
	QueryID string `json:"queryId"`

	Columns []queryColumn `json:"columns"`
	Rows    [][]any       `json:"rows"`

	RowCount int64 `json:"rowCount"`

	// Truncated says the result met the row cap. A client that ignores this
	// shows a partial answer as a whole one, which is the failure Part 16
	// refused to ship.
	Truncated bool `json:"truncated"`

	// CacheStatus is hit, miss or uncached -- the same vocabulary the query
	// log records, so what the browser shows and what an operator reads
	// cannot disagree.
	CacheStatus string `json:"cacheStatus"`

	DurationMs int64 `json:"durationMs"`
}

type queryableConnection struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type queryableConnectionsResponse struct {
	Connections []queryableConnection `json:"connections"`
}

type schemaTable struct {
	Schema  string   `json:"schema"`
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

type connectionSchemaResponse struct {
	Tables []schemaTable `json:"tables"`

	// Synced says whether anything has been cataloged. False is not an error:
	// a connection nobody has synced has no schema to offer, and an editor
	// that said "no tables" would be reporting an empty database instead of an
	// unread one.
	Synced bool `json:"synced"`
}

// --- handler ---------------------------------------------------------------

/*
handleSchema returns what Pivot knows a connection contains, for completion.

Read from the *catalog* rather than from the source. Autocomplete fires on
every keystroke, and introspecting somebody's warehouse that often would be an
outage with a text cursor in front of it -- Part 19-b built the catalog so this
question has a cheap answer.

The cost of that is honesty about staleness: this is what the last sync saw,
which is why the response says whether there has been one. A table added five
minutes ago will not complete until the next sync, and an editor that implied
otherwise would have somebody convinced their table does not exist.

Tables marked gone are left out. A sync marks rather than deletes, because a
table disappears for reasons that are not "somebody dropped it" -- but
completing a name the source will reject helps nobody.
*/
func (h *QueryHandler) handleSchema(w http.ResponseWriter, r *http.Request) {
	connectionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, r, NewError(CodeValidationFailed, "that is not a connection id", err))

		return
	}

	tables, err := h.repos.Catalog.Tables(r.Context(), connectionID)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	columns, err := h.repos.Catalog.Columns(r.Context(), connectionID)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	byTable := make(map[uuid.UUID][]string, len(tables))
	for _, c := range columns {
		if c.RemovedAt.Valid {
			continue
		}

		byTable[c.TableID] = append(byTable[c.TableID], c.ColumnName)
	}

	out := make([]schemaTable, 0, len(tables))

	for _, table := range tables {
		if table.RemovedAt.Valid {
			continue
		}

		out = append(out, schemaTable{
			Schema:  table.SchemaName,
			Name:    table.TableName,
			Columns: byTable[table.ID],
		})
	}

	WriteJSON(r.Context(), w, http.StatusOK, connectionSchemaResponse{
		Tables: out,
		Synced: len(tables) > 0,
	})
}

/*
handleConnections lists the sources this caller could query.

Deliberately thin: an id, a slug, a name and a kind. No host, no username, and
nothing that could hold a password -- an editor needs to know which sources
exist and what to call them, and every other field is a detail that belongs to
the administration screens Part 26 builds.

Disabled connections are left out rather than shown greyed. A picker that
offers something which cannot work is a picker that produces a confusing error
instead of a shorter list.
*/
func (h *QueryHandler) handleConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.repos.Connections.List(r.Context())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	out := make([]queryableConnection, 0, len(conns))

	for _, c := range conns {
		if !bool(c.IsEnabled) {
			continue
		}

		out = append(out, queryableConnection{
			ID: c.ID.String(), Slug: c.Slug, Name: c.Name, Kind: c.Kind,
		})
	}

	WriteJSON(r.Context(), w, http.StatusOK, queryableConnectionsResponse{Connections: out})
}

/*
handleRun runs a statement and returns its rows.

Materialized rather than streamed, deliberately and for now. The pipeline
streams end to end and this endpoint does not preserve that: it reads the whole
result into memory to answer with one JSON document, which is the right shape
for an editor showing a page of rows and the wrong one for an export. The row
cap is what keeps that honest -- a result cannot exceed the connection's limit,
so "the whole result" is bounded by a number an administrator set.

Part 24-a builds export ([QueryHandler.handleExport]), which is where
streaming to the client belongs.
*/
func (h *QueryHandler) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runQueryRequest
	if err := Decode(w, r, &req); err != nil {
		return
	}

	connectionID, err := uuid.Parse(req.ConnectionID)
	if err != nil {
		WriteError(w, r, NewError(CodeValidationFailed, "connectionId is not an id", err))

		return
	}

	started := time.Now()

	execution, err := h.executor.Execute(r.Context(), query.Request{
		ConnectionID: connectionID,
		SQL:          req.SQL,
		MaxRows:      req.MaxRows,
	})
	if err != nil {
		WriteError(w, r, queryError(err))

		return
	}

	defer func() {
		if cerr := execution.Stream.Close(); cerr != nil {
			h.log.Warn("closing a query", "error", cerr)
		}
	}()

	rows, rerr := collect(execution)
	if rerr != nil {
		WriteError(w, r, queryError(rerr))

		return
	}

	WriteJSON(r.Context(), w, http.StatusOK, runQueryResponse{
		QueryID:     execution.LogID.String(),
		Columns:     describeColumns(execution.Columns()),
		Rows:        rows,
		RowCount:    int64(len(rows)),
		Truncated:   execution.Stream.Truncated(),
		CacheStatus: execution.CacheStatus,
		DurationMs:  time.Since(started).Milliseconds(),
	})
}

/*
collect reads the stream into memory.

Each row is copied. [connectors.Stream] says a row is valid only until the next
Next, because the driver reuses the backing array -- keeping the slice would
give a response whose every row is the last one read.
*/
func collect(execution *query.Execution) ([][]any, error) {
	rows := make([][]any, 0, 128)

	for execution.Stream.Next() {
		row := execution.Stream.Row()
		copied := make([]any, len(row))

		for i, cell := range row {
			copied[i] = jsonSafe(cell)
		}

		rows = append(rows, copied)
	}

	if err := execution.Stream.Err(); err != nil {
		return nil, err
	}

	return rows, nil
}

/*
jsonSafe renders a decoded cell as something JSON can carry without lying.

Bytes become a string rather than base64 noise, and a time becomes RFC 3339 so
the zone survives -- Part 19-a went to some trouble to distinguish an instant
from a wall-clock reading, and marshaling it as whatever encoding/json feels
like would throw that away at the last step.

Anything else is left alone: encoding/json handles numbers, strings, booleans
and nulls correctly, and a type nobody has mapped is better rendered by the
standard library than guessed at here.
*/
func jsonSafe(cell any) any {
	switch v := cell.(type) {
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return cell
	}
}

func describeColumns(columns []query.Column) []queryColumn {
	out := make([]queryColumn, 0, len(columns))

	for _, c := range columns {
		out = append(out, queryColumn{
			Name: c.Name, Type: c.Type, SourceType: c.SourceType,
		})
	}

	return out
}

/*
queryError maps a pipeline failure onto the error contract.

The distinctions are the ones a person in a browser acts on. A statement the
source rejected is theirs to fix and carries the source's own words, because
"syntax error at or near FROM" is the whole answer and paraphrasing it would
lose it. A full instance is worth retrying in a moment. Anything else is a
failure they cannot do anything about, and saying so is better than implying
the SQL was wrong.

Authorization failures are left to [WriteError], which already maps them --
this must not turn a denial into a 400 that reads as a typo.
*/
func queryError(err error) error {
	switch {
	case errors.Is(err, query.ErrTooBusy):
		return NewError(CodeQueryBusy, CodeQueryBusy.Summary(), err)

	case errors.Is(err, query.ErrEmptySQL), errors.Is(err, query.ErrNoConnection):
		return NewError(CodeQueryRejected, "There is no statement to run", err)

	case errors.Is(err, repo.ErrNotFound):
		return NewError(CodeNotFound, "No such connection", err)

	case errors.Is(err, authz.ErrDenied):
		return NewError(CodeForbidden, "You may not run raw SQL on this instance", err)

	case errors.Is(err, authz.ErrUnavailable):
		// Not a denial. The caller may well be permitted and Pivot could not
		// find out, and telling them 403 sends them to argue with an
		// administrator about a permission they already have.
		return NewError(CodeUnavailable, CodeUnavailable.Summary(), err)

	default:
		// The source's own message, which is the useful part, and where it
		// said the problem is when it said.
		failure := NewError(CodeQueryFailed, query.SourceMessage(err), err)
		failure.Position = query.SourcePosition(err)

		return failure
	}
}
