package export

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
writeJSON streams a result as an array of objects.

Written by hand rather than through [encoding/json] because the document is
produced a row at a time and never exists as a value: marshaling the result
would mean building it, and building it is the thing this package exists not to
do. Marshaling row by row would work and costs an allocation per cell -- some
seventy million of them on a ten-million-row export -- which is affordable and
still not worth paying for a document whose structure is three characters.

The string escaping that makes this safe is [appendJSONString], and it is
checked against [encoding/json] in the tests over a corpus that includes
control characters, quotes, emoji and invalid UTF-8. Hand-written escaping is
only defensible with that test next to it.
*/
func writeJSON(w io.Writer, columns []Column, rows Rows) (int64, error) {
	out := bufio.NewWriterSize(w, bufferSize)

	// Reused across every cell of every row, which is what keeps the
	// allocation count flat.
	scratch := make([]byte, 0, 64)

	names := make([][]byte, len(columns))
	for i, column := range columns {
		names[i] = appendJSONString(nil, column.Name)
	}

	if err := out.WriteByte('['); err != nil {
		return 0, err
	}

	var written int64

	for rows.Next() {
		if written > 0 {
			if err := out.WriteByte(','); err != nil {
				return written, err
			}
		}

		if _, err := out.WriteString("\n  {"); err != nil {
			return written, err
		}

		row := rows.Row()

		for i := range columns {
			if i > 0 {
				if err := out.WriteByte(','); err != nil {
					return written, err
				}
			}

			if _, err := out.Write(names[i]); err != nil {
				return written, err
			}

			if err := out.WriteByte(':'); err != nil {
				return written, err
			}

			var cell any
			if i < len(row) {
				cell = row[i]
			}

			scratch = appendJSONValue(scratch[:0], cell, columns[i].Kind)

			if _, err := out.Write(scratch); err != nil {
				return written, err
			}
		}

		if err := out.WriteByte('}'); err != nil {
			return written, err
		}

		written++
	}

	if err := rows.Err(); err != nil {
		return written, err
	}

	// A newline at the end, so the file ends the way a text file ends and a
	// shell prompt does not land on the closing bracket.
	if _, err := out.WriteString("\n]\n"); err != nil {
		return written, err
	}

	return written, out.Flush()
}

/*
appendJSONValue writes one cell as JSON.

The kind decides, not the Go type, for the same reason it does in [text].

Three cases are worth naming. A [datatype.Decimal] is written as a *string*,
because a JSON number is a float64 in every mainstream parser and Decimal is
the type that exists to say that is wrong -- the package doc argues it. A NaN
or an infinity is written as null, because JSON cannot carry either and a
parser handed `NaN` either rejects the document or invents a meaning. And a
[datatype.JSON] column is written through unchanged, so a document stored in
the source arrives as a document rather than as a string containing one.
*/
func appendJSONValue(dst []byte, value any, kind datatype.Kind) []byte {
	if value == nil {
		return append(dst, "null"...)
	}

	switch kind {
	case datatype.Decimal:
		rendered, null := text(value, kind)
		if null {
			return append(dst, "null"...)
		}

		return appendJSONString(dst, rendered)

	case datatype.JSON:
		return appendRawJSON(dst, value)

	case datatype.Boolean:
		if v, ok := value.(bool); ok {
			return strconv.AppendBool(dst, v)
		}

	case datatype.Integer:
		switch v := value.(type) {
		case int64:
			return strconv.AppendInt(dst, v, 10)
		case int:
			return strconv.AppendInt(dst, int64(v), 10)
		case int32:
			return strconv.AppendInt(dst, int64(v), 10)
		case uint64:
			return strconv.AppendUint(dst, v, 10)
		}

	case datatype.Float:
		switch v := value.(type) {
		case float64:
			return appendJSONFloat(dst, v)
		case float32:
			return appendJSONFloat(dst, float64(v))
		}
	}

	rendered, null := text(value, kind)
	if null {
		return append(dst, "null"...)
	}

	return appendJSONString(dst, rendered)
}

/*
appendRawJSON writes a JSON column's own document, or gives up and quotes it.

A source that says a column is JSON is usually right, but "usually" is not a
thing to build a document around: a value that does not parse would produce a
broken file that fails at the consumer with no clue where. So anything that is
not valid JSON is written as the string it is, which is at worst unhelpful and
never corrupt.
*/
func appendRawJSON(dst []byte, value any) []byte {
	var raw []byte

	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		rendered, null := text(value, datatype.JSON)
		if null {
			return append(dst, "null"...)
		}

		return appendJSONString(dst, rendered)
	}

	if !validJSON(raw) {
		return appendJSONString(dst, string(raw))
	}

	return append(dst, raw...)
}

func appendJSONFloat(dst []byte, v float64) []byte {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return append(dst, "null"...)
	}

	return strconv.AppendFloat(dst, v, 'g', -1, 64)
}

/*
appendJSONString writes a JSON string literal.

The escaping rules are RFC 8259's and they are short: a quote and a backslash
are escaped, everything below 0x20 must be escaped, and everything else is
UTF-8 that passes through. Invalid UTF-8 becomes the replacement character,
which is what [encoding/json] does -- a JSON document containing an invalid
byte sequence is not a JSON document, and the alternative to substituting is
producing a file nothing will read.

U+2028 and U+2029 are escaped as well. They are legal in JSON and illegal in a
JavaScript string literal, and a document that a browser cannot `eval` but a
parser can is the sort of difference that is discovered in production.
*/
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')

	for i := 0; i < len(s); {
		c := s[i]

		if c < utf8.RuneSelf {
			switch {
			case c == '"':
				dst = append(dst, '\\', '"')
			case c == '\\':
				dst = append(dst, '\\', '\\')
			case c == '\n':
				dst = append(dst, '\\', 'n')
			case c == '\r':
				dst = append(dst, '\\', 'r')
			case c == '\t':
				dst = append(dst, '\\', 't')
			case c < 0x20:
				dst = append(dst, '\\', 'u', '0', '0',
					hexDigit(c>>4), hexDigit(c&0xF))
			default:
				dst = append(dst, c)
			}

			i++

			continue
		}

		r, size := utf8.DecodeRuneInString(s[i:])

		if r == utf8.RuneError && size == 1 {
			dst = append(dst, `�`...)
			i++

			continue
		}

		if r == ' ' || r == ' ' {
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigit(byte(r&0xF)))
			i += size

			continue
		}

		dst = append(dst, s[i:i+size]...)
		i += size
	}

	return append(dst, '"')
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}

	return 'a' + (b - 10)
}

/*
validJSON asks whether a value is a document without building one.

[json.Valid] scans; it does not allocate the value. That matters here because
this runs once per cell of a JSON column and unmarshaling to check would put
the cost of parsing the whole export on the act of deciding how to write it.
*/
func validJSON(raw []byte) bool { return json.Valid(raw) }
