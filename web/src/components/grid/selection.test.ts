import { describe, expect, test } from "vitest";

import { all, at, bounds, contains, move, toTSV } from "./selection";
import { compare, kindOf, render } from "./cells";

/*
 * The selection model.
 *
 * Tested without a DOM because this is where the edge cases are: shrinking a
 * selection back towards its anchor, clamping at the edges, and a copy that
 * has to survive being pasted into a spreadsheet.
 */

describe("a rectangular selection", () => {
  test("normalizes whichever way it was dragged", () => {
    // Dragged up and to the left, so the focus is above and before the anchor.
    const upwards = { anchor: { row: 5, col: 5 }, focus: { row: 2, col: 1 } };

    expect(bounds(upwards)).toEqual({ top: 2, bottom: 5, left: 1, right: 5 });
  });

  test("knows what is inside it", () => {
    const selection = { anchor: { row: 1, col: 1 }, focus: { row: 3, col: 2 } };

    expect(contains(selection, 2, 1)).toBe(true);
    expect(contains(selection, 0, 1)).toBe(false);
    expect(contains(selection, 2, 3)).toBe(false);
  });

  /*
   * Shifting back towards the anchor shrinks rather than starting again.
   *
   * This is the property that makes keyboard selection feel right, and the
   * reason the model is an anchor and a focus rather than a set of cells.
   */
  test("shrinks when the focus moves back", () => {
    const extent = { rows: 10, cols: 10 };

    let selection = { anchor: { row: 2, col: 2 }, focus: { row: 5, col: 2 } };
    expect(bounds(selection).bottom).toBe(5);

    selection = { anchor: selection.anchor, focus: move(selection.focus, "up", extent) };
    expect(bounds(selection).bottom).toBe(4);
  });
});

describe("moving the cursor", () => {
  test("clamps rather than wrapping", () => {
    const extent = { rows: 3, cols: 3 };

    // Arrowing off the end stops. Wrapping would move somebody to the far side
    // of the result with one keypress they did not mean.
    expect(move({ row: 0, col: 0 }, "up", extent)).toEqual({ row: 0, col: 0 });
    expect(move({ row: 2, col: 2 }, "down", extent)).toEqual({ row: 2, col: 2 });
    expect(move({ row: 0, col: 0 }, "left", extent)).toEqual({ row: 0, col: 0 });
    expect(move({ row: 2, col: 2 }, "right", extent)).toEqual({ row: 2, col: 2 });
  });

  test("survives an empty grid", () => {
    expect(move({ row: 0, col: 0 }, "down", { rows: 0, cols: 0 })).toEqual({ row: 0, col: 0 });
    expect(bounds(all({ rows: 0, cols: 0 }))).toEqual({ top: 0, bottom: 0, left: 0, right: 0 });
  });
});

/*
 * Copy, which is the feature that decides whether people export or just copy.
 *
 * TSV rather than CSV: a spreadsheet pasting TSV puts each value in its own
 * cell with no dialog, and CSV either opens an import wizard or lands in one
 * column.
 */
describe("copying a selection", () => {
  const rows = [
    [1, "EMEA", null],
    [2, "APAC", "x"],
    [3, "AMER", ""],
  ];

  test("renders the selected rectangle as tab-separated rows", () => {
    const selection = { anchor: { row: 0, col: 0 }, focus: { row: 1, col: 1 } };

    expect(toTSV(selection, rows, (v) => render(v))).toBe("1\tEMEA\n2\tAPAC");
  });

  test("copies one cell as itself", () => {
    expect(toTSV(at(1, 1), rows, (v) => render(v))).toBe("APAC");
  });

  // A tab inside a value would shift every column after it by one. A space is
  // a worse value and a better paste.
  test("flattens a tab inside a value", () => {
    const awkward = [["a\tb\nc"]];

    expect(toTSV(at(0, 0), awkward, (v) => render(v))).toBe("a b c");
  });

  test("carries a NULL as the word", () => {
    expect(toTSV(at(0, 2), rows, (v) => render(v))).toBe("NULL");
  });
});

describe("what a cell is", () => {
  /*
   * Three states that all look like an empty cell.
   *
   * "Is this missing or blank" is a question people ask of real data
   * constantly, and a grid that renders NULL, "" and the text "NULL"
   * identically cannot answer it.
   */
  test("tells a null from an empty string from the word", () => {
    expect(kindOf(null)).toBe("null");
    expect(kindOf("")).toBe("empty");
    expect(kindOf("NULL")).toBe("text");
  });

  test("renders values as themselves", () => {
    expect(render(null)).toBe("NULL");
    expect(render(true)).toBe("true");
    expect(render(0)).toBe("0");
    expect(render({ a: 1 })).toBe('{"a":1}');
  });
});

describe("sorting", () => {
  // Nulls last whichever way, because "the biggest" and "the smallest" are
  // both questions about values and a screen of NULLs answers neither.
  test("puts nulls last in both directions", () => {
    expect(compare(null, 5, true)).toBeGreaterThan(0);
    expect(compare(5, null, true)).toBeLessThan(0);
    expect(compare(null, null, true)).toBe(0);
  });

  test("compares numbers as numbers", () => {
    expect(compare(9, 10, true)).toBeLessThan(0);

    // And as text when the column is not numeric, where 10 sorts before 9 in
    // a plain string compare -- localeCompare's numeric option keeps that sane.
    expect(compare("9", "10", false)).toBeLessThan(0);
  });
});

/*
 * Dates and times, shown as what they are.
 *
 * A DATE arrives on the wire as `2026-09-26T00:00:00Z`, and rendering that
 * midnight and that zone is a grid telling somebody their date has a time in
 * it. It does not. Part 19-a distinguished a date from an instant from a
 * wall-clock reading; this is the last place that distinction is worth
 * anything, and the easiest place to throw it away.
 */
describe("showing dates and times", () => {
  test("a date is a date", () => {
    expect(render("2026-09-26T00:00:00Z", "date")).toBe("2026-09-26");
  });

  /*
   * A naive timestamp gets no zone.
   *
   * Adding one would claim the reading is UTC, which is exactly the error the
   * kind exists to prevent -- and MySQL's DATETIME and PostgreSQL's TIMESTAMP
   * are both this.
   */
  test("a wall-clock reading carries no zone", () => {
    expect(render("2026-02-21T13:43:17.433", "timestamp")).toBe("2026-02-21 13:43:17");
  });

  test("an instant keeps its zone", () => {
    expect(render("2026-02-21T13:43:17Z", "timestamp_tz")).toBe("2026-02-21 13:43:17 UTC");
    expect(render("2026-02-21T13:43:17+05:45", "timestamp_tz")).toBe("2026-02-21 13:43:17 +05:45");
  });

  test("a time is a time", () => {
    expect(render("13:43:17.433", "time")).toBe("13:43:17.433");
  });

  /*
   * Read as text, never through Date.
   *
   * Parsing into a Date and formatting back would apply the *viewer's* zone,
   * silently moving every value by the offset between them -- a row stamped
   * 00:30 UTC showing as the previous day in New York. The source said what it
   * meant.
   */
  test("does not shift a value into the reader's zone", () => {
    expect(render("2026-01-01T00:30:00Z", "timestamp_tz")).toContain("2026-01-01");
    expect(render("2026-01-01T00:30:00Z", "date")).toBe("2026-01-01");
  });

  test("leaves anything else alone", () => {
    expect(render("hello", "string")).toBe("hello");
    expect(render(42, "integer")).toBe("42");
    expect(render(null, "date")).toBe("NULL");
  });
});
