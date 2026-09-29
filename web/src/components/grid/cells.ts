/**
 * How a value is shown, and how it lines up.
 *
 * Separate from the grid because these are the decisions somebody reading an
 * answer actually notices, and they are worth being able to test on their own.
 */

/**
 * The canonical kinds that are numbers.
 *
 * Right alignment is not decoration: a column of right-aligned numbers can be
 * scanned for magnitude at a glance, and the same column left-aligned cannot.
 * It is the single most useful thing a grid does to a numeric column.
 */
const NUMERIC = new Set(["integer", "decimal", "float", "number"]);

export function isNumeric(kind: string): boolean {
  return NUMERIC.has(kind);
}

/**
 * What a cell shows, as text.
 *
 * The canonical kind decides, because the wire format does not: a DATE arrives
 * as `2026-09-26T00:00:00Z`, and showing that midnight and that zone is a grid
 * telling somebody their date has a time in it. It does not. Part 19-a went to
 * real trouble to distinguish a date from an instant from a wall-clock
 * reading, and this is the last place that distinction is worth anything.
 */
export function render(value: unknown, kind = ""): string {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "boolean") return value ? "true" : "false";

  switch (kind) {
    case "date":
      return datePart(value);

    case "timestamp":
      // A wall-clock reading. No zone, because it does not have one -- adding
      // a Z would claim it is UTC, which is the error this kind exists to
      // prevent.
      return `${datePart(value)} ${timePart(value)}`;

    case "timestamp_tz":
      return `${datePart(value)} ${timePart(value)}${zonePart(value)}`;

    case "time":
      return timePart(value);

    default:
      break;
  }

  if (typeof value === "object") return JSON.stringify(value);

  return String(value);
}

/*
 * The parts of an ISO instant, taken as text rather than through Date.
 *
 * Parsing into a Date and formatting back out would apply the *viewer's* zone,
 * which silently moves every timestamp by the offset between them -- a row
 * stamped 00:30 UTC showing as the previous day in New York. The source said
 * what it meant; this shows what the source said.
 */
function datePart(value: unknown): string {
  const text = String(value);
  const t = text.indexOf("T");

  return t > 0 ? text.slice(0, t) : text;
}

function timePart(value: unknown): string {
  const text = String(value);
  const t = text.indexOf("T");
  if (t < 0) return text;

  const rest = text.slice(t + 1);
  const cut = rest.search(/[Z+-]/);
  const clock = cut > 0 ? rest.slice(0, cut) : rest;

  // Seconds are worth keeping and sub-seconds usually are not: a column of
  // `.433` repeated is noise, and the value is one hover away.
  return clock.replace(/\.\d+$/, "");
}

function zonePart(value: unknown): string {
  const text = String(value);
  const rest = text.slice(text.indexOf("T") + 1);
  const cut = rest.search(/[Z+-]/);

  if (cut < 0) return "";

  const zone = rest.slice(cut);

  return zone === "Z" ? " UTC" : ` ${zone}`;
}

/**
 * What kind of thing a cell is, for styling.
 *
 * Three states that all look like an empty cell if nothing distinguishes them:
 * a NULL, an empty string, and -- for the person who has been bitten by this
 * before -- a value that is literally the text "NULL". A grid that renders all
 * three the same way is one somebody cannot trust to answer "is this missing
 * or blank", which is a question people ask of real data constantly.
 */
export type CellKind = "null" | "empty" | "text";

export function kindOf(value: unknown): CellKind {
  if (value === null || value === undefined) return "null";
  if (value === "") return "empty";

  return "text";
}

/**
 * A width for a column, in pixels, from what it holds.
 *
 * Sampled rather than measured over everything: a hundred rows is enough to
 * settle a column's usual width, and measuring a hundred thousand would cost
 * more than the answer is worth. A long value further down is handled by the
 * cell truncating, not by the column being wide enough for the worst case --
 * one 4000-character JSON blob should not push every other column off screen.
 */
/** What a column may narrow or widen to once the browser has sized it. */
export const MIN_WIDTH = 72;
export const MAX_WIDTH = 420;

/**
 * Compare two cells for sorting.
 *
 * NULLs sort last whichever way the column is sorted, because "show me the
 * biggest" and "show me the smallest" are both questions about values, and a
 * screen of NULLs is not an answer to either.
 */
export function compare(a: unknown, b: unknown, numeric: boolean): number {
  const aNull = a === null || a === undefined;
  const bNull = b === null || b === undefined;

  if (aNull && bNull) return 0;
  if (aNull) return 1;
  if (bNull) return -1;

  if (numeric) {
    const left = Number(a);
    const right = Number(b);

    if (!Number.isNaN(left) && !Number.isNaN(right)) return left - right;
  }

  return String(a).localeCompare(String(b), undefined, { numeric: true });
}
