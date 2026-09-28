package conformance

import (
	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
A stream over a result already in memory.

What the fakes in this package return. Trivial by design: the suite's job is to
check that a *connector* streams correctly, and the fakes exist to check the
suite. A fake with a clever stream would be testing itself.

It does honor the one part of the contract a caller can get wrong in either
direction -- Row is only valid until the next Next -- by handing out the same
backing slice each time, which is what a real driver does and what would hide a
caller that forgot to copy.
*/
type sliceStream struct {
	result *connectors.Result
	at     int
	row    []any
	closed bool
	failAt int
	err    error
}

func newSliceStream(result *connectors.Result) *sliceStream {
	width := len(result.Columns)

	return &sliceStream{result: result, row: make([]any, width), failAt: -1}
}

func (s *sliceStream) Columns() []connectors.Column { return s.result.Columns }

func (s *sliceStream) Next() bool {
	if s.closed || s.err != nil || s.at >= len(s.result.Rows) {
		return false
	}

	if s.at == s.failAt {
		s.err = connectors.Errorf(connectors.ReasonUnknown, nil, "",
			"the fake stream failed at row %d", s.at)

		return false
	}

	// Copied into one reused slice, exactly as a driver reusing its buffer
	// would: a caller that keeps Row without copying sees it change.
	copy(s.row, s.result.Rows[s.at])
	s.at++

	return true
}

func (s *sliceStream) Row() []any { return s.row }

func (s *sliceStream) Err() error { return s.err }

func (s *sliceStream) Truncated() bool { return s.result.Truncated }

func (s *sliceStream) Close() error {
	s.closed = true

	return nil
}
