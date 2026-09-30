package api

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
	"github.com/Mmd4LIFE/pivot/internal/export"
	"github.com/Mmd4LIFE/pivot/internal/query"
)

/*
handleExport runs a statement and streams the result as a file.

The same door as [QueryHandler.handleRun] -- the pipeline, nothing else -- and
the opposite shape: rows leave as they arrive rather than waiting for the last
one. That is what makes a multi-million-row CSV a constant-memory operation on
the server, which is the Done-when for Part 24-a and which handleRun cannot
promise because it answers with one JSON document.

Status and the error envelope are decided *before* the first row is written.
Once the stream starts, a source failure mid-download can only truncate the
file; the status line is already gone. That trade-off is the one every
streaming download makes, and it is why the writers report how many rows they
got out -- so the log can say where it stopped.
*/
func (h *QueryHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if err := Decode(w, r, &req); err != nil {
		return
	}

	format, err := export.ParseFormat(req.Format)
	if err != nil {
		WriteError(w, r, NewError(CodeValidationFailed, err.Error(), err))

		return
	}

	connectionID, err := uuid.Parse(req.ConnectionID)
	if err != nil {
		WriteError(w, r, NewError(CodeValidationFailed, "connectionId is not an id", err))

		return
	}

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
			h.log.Warn("closing an export", "error", cerr)
		}
	}()

	columns := exportColumns(execution.Columns())

	filename := fmt.Sprintf("result.%s", format.Extension())

	w.Header().Set("Content-Type", format.ContentType())
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, filename))
	// A download is not a document a CDN should keep: the SQL that produced it
	// may read different rows a second later, and caching one person's answer
	// as another person's file is how that happens quietly.
	w.Header().Set("Cache-Control", "no-store")

	// Headers out before the first byte of body, so a client that starts
	// writing to disk does not have to wait for the last row to know the name.
	w.WriteHeader(http.StatusOK)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	written, werr := export.Write(w, format, columns, execution.Stream)
	if werr != nil {
		// Bytes are already on the wire. The status cannot change; what we can
		// do is stop and say how far we got.
		h.log.Error("export failed mid-stream",
			"error", werr,
			"format", format,
			"rows", written,
			"query_id", execution.LogID,
		)
	}
}

type exportRequest struct {
	ConnectionID string `json:"connectionId"`
	SQL          string `json:"sql"`
	Format       string `json:"format"`

	// MaxRows caps this result. Zero uses the connection's own limit.
	MaxRows int64 `json:"maxRows,omitempty"`
}

func exportColumns(columns []query.Column) []export.Column {
	out := make([]export.Column, len(columns))

	for i, c := range columns {
		out[i] = export.Column{
			Name: c.Name,
			Kind: datatype.Kind(c.Type),
		}
	}

	return out
}
