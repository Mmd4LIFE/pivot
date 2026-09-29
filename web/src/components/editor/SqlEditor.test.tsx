import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";

import { SqlEditor } from "./SqlEditor";

/*
 * The editor keeps the cursor.
 *
 * This exists because it did not. The first version listed the completion
 * schema in its effect dependencies, and that schema was a fresh object on
 * every render -- so every keystroke destroyed the CodeMirror view and built a
 * new one, and the caret vanished after each character. It was reported by
 * somebody using it, not caught here, which is why there is now a test that
 * asks the only question that matters: is this the same editor it was a moment
 * ago?
 */

afterEach(cleanup);

/** The DOM node CodeMirror owns, which a rebuild would replace. */
function contentNode() {
  return screen.getByTestId("sql-editor").querySelector(".cm-content");
}

describe("the editor survives", () => {
  test("a re-render with a new schema object", () => {
    const onChange = vi.fn();

    const { rerender } = render(
      <SqlEditor value="SELECT 1" onChange={onChange} dialect="postgres" schema={{ a: ["x"] }} />,
    );

    const before = contentNode();
    expect(before).toBeTruthy();

    // A parent re-rendering with an equal-but-new schema object, which is what
    // every keystroke used to do.
    rerender(
      <SqlEditor value="SELECT 1" onChange={onChange} dialect="postgres" schema={{ a: ["x"] }} />,
    );

    expect(contentNode()).toBe(before);
  });

  /*
   * And a real change of source.
   *
   * Switching a tab's connection reconfigures the language rather than
   * rebuilding, so somebody who changes source mid-query keeps their place.
   */
  test("a change of dialect", () => {
    const onChange = vi.fn();

    const { rerender } = render(
      <SqlEditor value="SELECT 1" onChange={onChange} dialect="postgres" />,
    );

    const before = contentNode();

    rerender(<SqlEditor value="SELECT 1" onChange={onChange} dialect="mysql" />);

    expect(contentNode()).toBe(before);
  });

  // A new callback identity is the other thing a parent produces every render.
  test("a new onChange on every render", () => {
    const { rerender } = render(
      <SqlEditor value="SELECT 1" onChange={() => {}} dialect="sqlite" />,
    );

    const before = contentNode();

    rerender(<SqlEditor value="SELECT 1" onChange={() => {}} dialect="sqlite" />);

    expect(contentNode()).toBe(before);
  });
});

/*
 * A document that changed underneath the editor is pushed in.
 *
 * That is what switching tabs looks like from here, and it is the one case
 * where the editor's content must be replaced rather than left alone.
 */
test("takes a document that changed underneath it", () => {
  const { rerender } = render(
    <SqlEditor value="SELECT 1" onChange={() => {}} dialect="sqlite" />,
  );

  expect(contentNode()?.textContent).toContain("SELECT 1");

  rerender(<SqlEditor value="SELECT 2" onChange={() => {}} dialect="sqlite" />);

  expect(contentNode()?.textContent).toContain("SELECT 2");
});
