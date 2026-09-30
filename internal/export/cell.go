package export

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/datatype"
)

/*
Layouts, one per temporal kind.

A DATE is a calendar day and is written as one. A [datatype.Timestamp] has no
zone -- it is a wall-clock reading, a moment on a calendar rather than a moment
in history -- so writing it with a `Z` on the end would be inventing
information. A [datatype.TimestampTZ] is an instant and keeps its offset.

The space between the date and the time, rather than a `T`, is deliberate for
the zoneless kinds: it is what every SQL console and every spreadsheet writes,
and the `T` in RFC 3339 exists to introduce a zone that is not there.
*/
const (
	dateLayout      = "2006-01-02"
	timeLayout      = "15:04:05.999999999"
	timestampLayout = "2006-01-02 15:04:05.999999999"
)

/*
text renders a cell for a delimited format, and says whether it was NULL.

The bool is the whole reason this does not just return a string: a NULL and an
empty string both render as no characters, and the caller needs to know which
one it is holding to decide whether to quote it. See the package doc.

Nothing here inspects the value to decide what it means -- the kind comes from
the column. That is what makes an empty result set still format its dates like
dates, and it is what the Done-when means by formatting driven by the canonical
types rather than guessed from values.
*/
func text(value any, kind datatype.Kind) (string, bool) {
	if value == nil {
		return "", true
	}

	switch v := value.(type) {
	case time.Time:
		return v.Format(layoutFor(kind)), false

	case []byte:
		/*
		 * Bytes mean two entirely different things depending on the column.
		 *
		 * The MySQL driver hands back []byte for text, numerics and dates
		 * alike, so treating every []byte as binary would base64 most of a
		 * result. Only a column the catalog calls Binary is actually bytes.
		 */
		if kind == datatype.Binary {
			return base64.StdEncoding.EncodeToString(v), false
		}

		return string(v), false

	case string:
		return v, false

	case bool:
		return strconv.FormatBool(v), false

	case float64:
		return formatFloat(v), false

	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), false

	case int64:
		return strconv.FormatInt(v, 10), false

	case int:
		return strconv.Itoa(v), false

	case int32:
		return strconv.FormatInt(int64(v), 10), false

	case uint64:
		return strconv.FormatUint(v, 10), false

	default:
		// A type nobody has mapped is printed by the standard library rather
		// than guessed at here. datatype.Unknown is a real answer and this is
		// what it looks like on the way out.
		return fmt.Sprint(v), false
	}
}

func layoutFor(kind datatype.Kind) string {
	switch kind {
	case datatype.Date:
		return dateLayout
	case datatype.Time:
		return timeLayout
	case datatype.Timestamp:
		return timestampLayout
	default:
		// TimestampTZ, and anything temporal nobody has classified: keep the
		// offset. Dropping a zone is unrecoverable; carrying one that was not
		// wanted is merely noisy.
		return time.RFC3339Nano
	}
}

/*
formatFloat writes the shortest text that reads back as the same float64.

'g' with -1 precision rather than a fixed number of places: a fixed format
either truncates (silently changing the value) or pads (implying precision the
float never had). The exponent form it produces for very large and very small
numbers is correct and is what every parser expects.
*/
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
