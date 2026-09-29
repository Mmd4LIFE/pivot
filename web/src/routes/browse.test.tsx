import { describe, expect, test } from "vitest";

import { previewStatement } from "./browse.connection";

/*
 * The statement that opens a table.
 *
 * Quoting is the whole of it. An unquoted identifier breaks on the first table
 * somebody named `order`, `select` or `Group By Results` -- and those exist in
 * every warehouse that has been around for a while.
 */
describe("opening a table", () => {
  test("qualifies and quotes for most sources", () => {
    expect(previewStatement({ schema: "demo", name: "invoices", columns: [] }, "postgres")).toBe(
      'SELECT *\nFROM "demo"."invoices"\nLIMIT 100',
    );
  });

  // MySQL is the odd one out and always has been.
  test("uses backticks for MySQL", () => {
    expect(previewStatement({ schema: "app", name: "users", columns: [] }, "mysql")).toBe(
      "SELECT *\nFROM `app`.`users`\nLIMIT 100",
    );
  });

  test("leaves the schema off when there is not one", () => {
    expect(previewStatement({ schema: "", name: "orders", columns: [] }, "sqlite")).toBe(
      'SELECT *\nFROM "orders"\nLIMIT 100',
    );
  });

  /*
   * A quote inside an identifier is doubled, not stripped.
   *
   * Rare, legal, and the one case where getting it wrong turns a preview into
   * a syntax error somebody cannot explain -- or worse, into a statement that
   * means something else.
   */
  test("doubles a quote inside a name", () => {
    expect(previewStatement({ schema: "", name: 'we"ird', columns: [] }, "postgres")).toBe(
      'SELECT *\nFROM "we""ird"\nLIMIT 100',
    );

    expect(previewStatement({ schema: "", name: "we`ird", columns: [] }, "mysql")).toBe(
      "SELECT *\nFROM `we``ird`\nLIMIT 100",
    );
  });

  /*
   * A LIMIT, always.
   *
   * "Show me this table" means the first screen of it. Without one, clicking a
   * hundred-million-row table is a mistake that takes a warehouse with it.
   */
  test("always limits", () => {
    expect(previewStatement({ schema: "s", name: "t", columns: [] }, "postgres")).toContain(
      "LIMIT 100",
    );
  });
});
