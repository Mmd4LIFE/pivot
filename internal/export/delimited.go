package export

import (
	"bufio"
	"io"
	"strings"
)

/*
The buffer, and the only thing here whose size does not depend on the answer.

64 KiB is large enough that a row costs no syscall and small enough that a
thousand concurrent exports is 64 MB rather than a number worth worrying
about.
*/
const bufferSize = 64 << 10

/*
writeDelimited streams a result as CSV or TSV.

RFC 4180's rules with a configurable delimiter, plus the one convention the RFC
has nothing to say about: a NULL is an empty field with no quotes and an empty
string is a quoted empty field, which is how PostgreSQL's `COPY ... CSV` keeps
them apart. The package doc has the reasoning.

Line endings are CRLF, which RFC 4180 specifies and Excel expects. Every tool
that reads CSV on a Unix system reads CRLF; not every tool on Windows reads
bare LF.
*/
func writeDelimited(w io.Writer, delimiter byte, columns []Column, rows Rows) (int64, error) {
	out := bufio.NewWriterSize(w, bufferSize)

	for i, column := range columns {
		if i > 0 {
			if err := out.WriteByte(delimiter); err != nil {
				return 0, err
			}
		}

		// A header is a value, never a NULL: a column named "" is still a
		// column, and an unquoted empty field is how this format spells NULL.
		if err := writeField(out, column.Name, delimiter); err != nil {
			return 0, err
		}
	}

	if _, err := out.WriteString("\r\n"); err != nil {
		return 0, err
	}

	var written int64

	for rows.Next() {
		row := rows.Row()

		for i := range columns {
			if i > 0 {
				if err := out.WriteByte(delimiter); err != nil {
					return written, err
				}
			}

			/*
			 * A row shorter than the header is a connector bug rather than a
			 * thing to crash on. An absent cell is written as NULL, which is
			 * the truthful answer: nothing arrived.
			 */
			var cell any
			if i < len(row) {
				cell = row[i]
			}

			value, null := text(cell, columns[i].Kind)
			if null {
				continue
			}

			if err := writeField(out, value, delimiter); err != nil {
				return written, err
			}
		}

		if _, err := out.WriteString("\r\n"); err != nil {
			return written, err
		}

		written++
	}

	if err := rows.Err(); err != nil {
		return written, err
	}

	return written, out.Flush()
}

/*
writeField writes one value, quoting it when it has to be quoted.

Quoted when it contains the delimiter, a quote, a carriage return or a
newline -- and when it is empty, because an unquoted empty field is how this
format spells NULL. A caller with a NULL does not call this at all; it writes
nothing and moves to the next delimiter.
*/
func writeField(out *bufio.Writer, value string, delimiter byte) error {
	if !needsQuoting(value, delimiter) {
		_, err := out.WriteString(value)

		return err
	}

	if err := out.WriteByte('"'); err != nil {
		return err
	}

	// Escaped by doubling, which is RFC 4180's rule and not a backslash.
	for {
		at := strings.IndexByte(value, '"')
		if at < 0 {
			break
		}

		if _, err := out.WriteString(value[:at]); err != nil {
			return err
		}

		if _, err := out.WriteString(`""`); err != nil {
			return err
		}

		value = value[at+1:]
	}

	if _, err := out.WriteString(value); err != nil {
		return err
	}

	return out.WriteByte('"')
}

func needsQuoting(value string, delimiter byte) bool {
	if value == "" {
		return true
	}

	for i := range len(value) {
		switch value[i] {
		case delimiter, '"', '\r', '\n':
			return true
		}
	}

	return false
}
