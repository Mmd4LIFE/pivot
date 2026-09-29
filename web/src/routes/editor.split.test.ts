import { beforeEach, describe, expect, test } from "vitest";
import { act, renderHook } from "@testing-library/react";

import {
  DEFAULT_EDITOR_HEIGHT,
  MAX_EDITOR_HEIGHT,
  MIN_EDITOR_HEIGHT,
  clampHeight,
  clearSplit,
  useSplit,
} from "./editor.split";

/*
 * The split between the editor and the results.
 *
 * Which half somebody wants larger is not something a product can know:
 * writing a long query and reading a wide result want opposite layouts, and
 * the same person switches between them several times an hour. A split that
 * reset on reload would be a preference expressed and then ignored.
 */

beforeEach(() => {
  clearSplit();
});

describe("the split", () => {
  test("starts somewhere usable", () => {
    expect(renderHook(() => useSplit()).result.current.height).toBe(DEFAULT_EDITOR_HEIGHT);
  });

  test("survives a reload", () => {
    const first = renderHook(() => useSplit());

    act(() => first.result.current.nudge(96));
    const moved = first.result.current.height;

    first.unmount();

    expect(renderHook(() => useSplit()).result.current.height).toBe(moved);
  });

  // Neither half can be dragged away entirely: an editor with no height is a
  // page somebody cannot type on, and there is no way back from it.
  test("cannot be collapsed or run away", () => {
    expect(clampHeight(-500)).toBe(MIN_EDITOR_HEIGHT);
    expect(clampHeight(99999)).toBe(MAX_EDITOR_HEIGHT);
  });

  test("survives nonsense in storage", () => {
    window.localStorage.setItem("pivot.editor.split.v1", "not a number");

    expect(renderHook(() => useSplit()).result.current.height).toBe(DEFAULT_EDITOR_HEIGHT);
  });

  test("nudges by keyboard within the bounds", () => {
    const { result } = renderHook(() => useSplit());

    act(() => result.current.nudge(-10_000));

    expect(result.current.height).toBe(MIN_EDITOR_HEIGHT);
  });
});
