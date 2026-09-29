import { describe, expect, test } from "vitest";

import { decodeQuestion, encodeQuestion, questionLink } from "./editor.link";

/*
 * A question carried in a link.
 *
 * The round trip is the easy half. The half worth testing is everything that
 * arrives instead of a well-formed link: a truncated paste, a chat client that
 * wrapped the line, a version from a later release, somebody's own hash.
 */

describe("a shared question", () => {
  test("survives the round trip", () => {
    const question = { connectionId: "c1", sql: "SELECT 1", title: "Sizes" };

    expect(decodeQuestion(encodeQuestion(question))).toEqual({ v: 1, ...question });
  });

  test("keeps newlines and quotes intact", () => {
    const sql = `SELECT *\nFROM "demo"."invoices"\nWHERE note = 'it''s here'\nLIMIT 100`;

    expect(decodeQuestion(encodeQuestion({ connectionId: "c1", sql }))?.sql).toBe(sql);
  });

  /*
   * The case btoa cannot do on its own.
   *
   * `btoa` takes Latin-1 and throws above U+00FF, so a share button that
   * skipped the UTF-8 step would work until somebody queried a column named
   * `città` -- which is worse than not having one.
   */
  test("carries text that is not Latin-1", () => {
    const sql = 'SELECT "città", "顧客", "café" FROM "naïve" WHERE x = \'→\'';

    const decoded = decodeQuestion(encodeQuestion({ connectionId: "c1", sql }));

    expect(decoded?.sql).toBe(sql);
  });

  // The fragment ends up in a URL people paste into chat, where `+` is the
  // character most likely to come back as a space.
  test("encodes into the URL-safe alphabet", () => {
    const encoded = encodeQuestion({
      connectionId: "c1",
      sql: "SELECT '?????????????????' FROM t",
    });

    expect(encoded).not.toMatch(/[+/=]/);
  });

  test("builds an absolute link from the page's own origin", () => {
    const link = questionLink({ connectionId: "c1", sql: "SELECT 1" }, "https://pivot.example");

    expect(link.startsWith("https://pivot.example/editor#")).toBe(true);
  });
});

describe("what arrives instead of a link", () => {
  test("nothing", () => {
    expect(decodeQuestion("")).toBeNull();
    expect(decodeQuestion("#")).toBeNull();
    expect(decodeQuestion("   ")).toBeNull();
  });

  test("somebody else's hash", () => {
    expect(decodeQuestion("#section-3")).toBeNull();
    expect(decodeQuestion("not-base64-at-all!!")).toBeNull();
  });

  // A chat client that wrapped the line, or a paste that lost the end.
  test("a truncated one", () => {
    const whole = encodeQuestion({ connectionId: "c1", sql: "SELECT 1" });

    expect(decodeQuestion(whole.slice(0, whole.length - 8))).toBeNull();
  });

  /*
   * A version this release does not know.
   *
   * A link somebody bookmarked outlives the code that made it, which is why
   * there is a version at all -- and why an unknown one is refused rather than
   * read hopefully.
   */
  test("a version from the future", () => {
    const later = btoa(JSON.stringify({ v: 2, connectionId: "c1", sql: "SELECT 1" }))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");

    expect(decodeQuestion(later)).toBeNull();
  });

  test("valid base64 that is not a question", () => {
    const notAQuestion = btoa('{"hello":"world"}').replace(/=+$/, "");

    expect(decodeQuestion(notAQuestion)).toBeNull();
  });

  test("a question with no connection", () => {
    const missing = btoa(JSON.stringify({ v: 1, sql: "SELECT 1" })).replace(/=+$/, "");

    expect(decodeQuestion(missing)).toBeNull();
  });

  // A leading # is how location.hash arrives, so both forms have to work.
  test("either with or without the hash", () => {
    const encoded = encodeQuestion({ connectionId: "c1", sql: "SELECT 1" });

    expect(decodeQuestion(encoded)).toEqual(decodeQuestion(`#${encoded}`));
  });
});
