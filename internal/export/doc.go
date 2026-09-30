/*
Package export writes a result out of Pivot, one row at a time.

# Nothing is accumulated

Every writer here reads a row, writes it, and forgets it. There is no slice of
rows, no [bytes.Buffer] holding the document, and no length to compute before
the first byte goes out -- which is what makes a ten-million-row export a
constant-memory operation rather than a way to take the server down. The
budget is a [bufio.Writer] and one row, and it does not grow with the answer.

That is the property [TestMemoryDoesNotGrowWithRows] measures, and the reason
it compares two sizes rather than asserting a number: a ceiling is a guess
about the machine, and flatness is the actual claim.

# NULL is not an empty string

The connector conformance suite has a named property keeping those two apart
across four databases, and an export that collapsed them at the last step
would undo it. So the delimited writers follow PostgreSQL's `COPY ... CSV`
convention: a NULL is an empty field with no quotes, and an empty string is a
quoted empty field.

	id,note
	1,
	2,""

Row 1's note is NULL. Row 2's is the empty string. Most readers will collapse
them again -- pandas reads both as NaN unless told otherwise -- but the file is
the artifact Pivot is responsible for, and it is right in the file.

# Formatting comes from the canonical type

A DATE is written `2026-09-26`, not `2026-09-26T00:00:00Z`: the midnight is an
artifact of transport and printing it tells somebody their date has a time in
it. A [datatype.Timestamp] has no zone and is written without one; a
[datatype.TimestampTZ] is an instant and keeps its offset. Part 19-a went to
real trouble to make that distinction, and this is one of the places it would
be easiest to throw away.

None of it is guessed from the value. The kind comes from the column, which
comes from the catalog and the driver -- so an empty column of dates formats
like dates.

# Decimals are written as JSON strings

A JSON number is a float64 in every mainstream parser, and [datatype.Decimal]
exists precisely to mark the values where that is wrong: a NUMERIC(19,4) total
that round-trips through a double is how a ledger stops balancing. So the JSON
writer emits a Decimal as a string, and a consumer that wants arithmetic parses
it with something that can do arithmetic.

This is the one place the format is deliberately less convenient than it could
be. The alternative is silent corruption of exactly the column people export to
check the numbers.
*/
package export
