import { beforeEach, describe, expect, test } from "vitest";
import { act, renderHook } from "@testing-library/react";

import { clearWorkspace, emptyWorkspace, useWorkspace } from "./editor.workspace";

/*
 * The tabs, and the reload they have to survive.
 *
 * Losing a half-written query to a refresh is the thing people never forgive,
 * and it is what makes somebody keep a text file open beside the tool instead
 * of using it. So the interesting tests here are the ones about coming back:
 * what happens on reload, and what happens when what was stored is not what
 * this version writes.
 */

beforeEach(() => {
  clearWorkspace();
});

describe("a workspace", () => {
  test("starts with one tab holding something runnable", () => {
    const { result } = renderHook(() => useWorkspace());

    expect(result.current.workspace.tabs).toHaveLength(1);
    expect(result.current.active.sql).toBe("SELECT 1");
  });

  test("opens and closes tabs", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.open());
    expect(result.current.workspace.tabs).toHaveLength(2);

    // A tab opened after the first starts empty: the question "does this
    // work" has been answered, and somebody is about to type their own.
    expect(result.current.active.sql).toBe("");

    const id = result.current.active.id;
    act(() => result.current.close(id));

    expect(result.current.workspace.tabs).toHaveLength(1);
  });

  /*
   * Closing the last tab leaves somewhere to type.
   *
   * A workspace with no tabs has no editor in it, and the state is reachable
   * in one click. Replacing it is what somebody meant by closing their only
   * tab anyway.
   */
  test("closing the last tab opens a fresh one", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.close(result.current.active.id));

    expect(result.current.workspace.tabs).toHaveLength(1);
    expect(result.current.active.sql).toBe("SELECT 1");
  });

  // The whole point.
  test("survives a reload", () => {
    const first = renderHook(() => useWorkspace());

    act(() => first.result.current.update({ sql: "SELECT * FROM half_written" }));
    act(() => first.result.current.open());
    act(() => first.result.current.update({ sql: "SELECT 2", connectionId: "c1" }));

    first.unmount();

    // A new mount reads what the last one wrote, which is what a refresh does.
    const second = renderHook(() => useWorkspace());

    expect(second.result.current.workspace.tabs).toHaveLength(2);
    expect(second.result.current.workspace.tabs[0]?.sql).toBe("SELECT * FROM half_written");
    expect(second.result.current.active.sql).toBe("SELECT 2");
    expect(second.result.current.active.connectionId).toBe("c1");
  });

  // Each tab keeps its own source, so two tabs can query two databases.
  test("keeps a source per tab", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.update({ connectionId: "warehouse" }));
    const firstId = result.current.active.id;

    act(() => result.current.open());
    act(() => result.current.update({ connectionId: "analytics" }));

    act(() => result.current.select(firstId));
    expect(result.current.active.connectionId).toBe("warehouse");
  });
});

describe("what a previous version left behind", () => {
  /*
   * Stored state is data an older Pivot wrote, and a shape change must not
   * present as a crash on the page somebody was in the middle of using.
   */
  test("survives nonsense in storage", () => {
    for (const junk of ["not json", "{}", '{"tabs":[]}', '{"tabs":"nope"}', "null"]) {
      window.localStorage.setItem("pivot.editor.workspace.v1", junk);

      const { result, unmount } = renderHook(() => useWorkspace());

      expect(result.current.workspace.tabs).toHaveLength(1);
      expect(result.current.active.id).toBeTruthy();

      unmount();
    }
  });

  // A stored tab missing the fields this version needs is dropped rather than
  // rendered as undefined.
  test("drops tabs it cannot read", () => {
    window.localStorage.setItem(
      "pivot.editor.workspace.v1",
      JSON.stringify({ tabs: [{ id: "a" }, { id: "b", title: "Kept", sql: "SELECT 1" }], activeId: "b" }),
    );

    const { result } = renderHook(() => useWorkspace());

    expect(result.current.workspace.tabs).toHaveLength(1);
    expect(result.current.active.title).toBe("Kept");
  });

  /*
   * An active id naming a tab that is gone falls back to one that exists.
   *
   * Otherwise the page has no active tab and renders nothing, which is a blank
   * screen produced by data rather than by code -- the kind that only appears
   * for the person who had that exact state.
   */
  test("falls back when the active tab is missing", () => {
    window.localStorage.setItem(
      "pivot.editor.workspace.v1",
      JSON.stringify({ tabs: [{ id: "a", title: "A", sql: "" }], activeId: "gone" }),
    );

    const { result } = renderHook(() => useWorkspace());

    expect(result.current.active.id).toBe("a");
  });
});

// A storage that throws costs the reload guarantee and nothing else. Refusing
// to let somebody type because their draft cannot be saved is the worse trade
// by a distance.
test("a blocked storage does not stop the editor", () => {
  const original = window.localStorage.setItem;
  window.localStorage.setItem = () => {
    throw new Error("quota");
  };

  try {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.update({ sql: "SELECT 1" }));

    expect(result.current.active.sql).toBe("SELECT 1");
  } finally {
    window.localStorage.setItem = original;
  }
});

test("emptyWorkspace is always usable", () => {
  const workspace = emptyWorkspace();

  expect(workspace.tabs).toHaveLength(1);
  expect(workspace.activeId).toBe(workspace.tabs[0]?.id);
});

describe("organising tabs", () => {
  test("renames a tab", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.rename(result.current.active.id, "Revenue by region"));

    expect(result.current.active.title).toBe("Revenue by region");
  });

  /*
   * An empty name falls back rather than leaving a tab with no label.
   *
   * A strip of unlabelled tabs is unusable, and clearing the field is what
   * somebody does on the way to typing something else.
   */
  test("refuses to leave a tab unlabelled", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.rename(result.current.active.id, "   "));

    expect(result.current.active.title).toBeTruthy();
  });

  test("a rename survives a reload", () => {
    const first = renderHook(() => useWorkspace());

    act(() => first.result.current.rename(first.result.current.active.id, "Kept"));
    first.unmount();

    expect(renderHook(() => useWorkspace()).result.current.active.title).toBe("Kept");
  });

  test("moves a tab along the strip", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.rename(result.current.active.id, "First"));
    act(() => result.current.open());
    act(() => result.current.rename(result.current.active.id, "Second"));

    const second = result.current.active.id;

    act(() => result.current.move(second, -1));

    expect(result.current.workspace.tabs.map((tab) => tab.title)).toEqual(["Second", "First"]);
  });

  // Moving off either end does nothing rather than wrapping or dropping.
  test("stops at the ends", () => {
    const { result } = renderHook(() => useWorkspace());

    const only = result.current.active.id;

    act(() => result.current.move(only, -1));
    act(() => result.current.move(only, 1));

    expect(result.current.workspace.tabs).toHaveLength(1);
    expect(result.current.active.id).toBe(only);
  });

  /*
   * A tab opened from a link or from Browse is added, never substituted.
   *
   * Whatever was already being written is somebody's work, and a link that
   * silently overwrote it would be the last link they clicked.
   */
  test("opens an arriving question beside what was there", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.update({ sql: "SELECT mine" }));

    act(() =>
      result.current.openWith({ title: "Shared", sql: "SELECT theirs", connectionId: "c1" }),
    );

    expect(result.current.workspace.tabs).toHaveLength(2);
    expect(result.current.workspace.tabs[0]?.sql).toBe("SELECT mine");
    expect(result.current.active.sql).toBe("SELECT theirs");
  });

  /*
   * The same question twice is the same tab.
   *
   * The address bar carries the statement as it is typed, so a reload hands
   * the page its own fragment back on mount. Appending unconditionally meant
   * every refresh opened a second copy of the tab somebody was already in --
   * and the refresh after that a third.
   */
  test("reuses the tab that already holds an arriving question", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() =>
      result.current.openWith({ title: "Shared", sql: "SELECT theirs", connectionId: "c1" }),
    );

    const opened = result.current.workspace.activeId;

    act(() =>
      result.current.openWith({ title: "Shared", sql: "SELECT theirs", connectionId: "c1" }),
    );

    expect(result.current.workspace.tabs).toHaveLength(2);
    expect(result.current.workspace.activeId).toBe(opened);
  });

  // Renaming is the one edit that must not make a link stop matching: the
  // title is the part somebody changes, the statement is the question.
  test("matches an arriving question past a rename", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() =>
      result.current.openWith({ title: "Shared", sql: "SELECT theirs", connectionId: "c1" }),
    );

    const opened = result.current.workspace.activeId;

    act(() => result.current.rename(opened, "Mine now"));

    act(() =>
      result.current.openWith({ title: "Shared", sql: "SELECT theirs", connectionId: "c1" }),
    );

    expect(result.current.workspace.tabs).toHaveLength(2);
    expect(result.current.workspace.activeId).toBe(opened);
  });

  // A different source is a different question, even with identical text --
  // "SELECT count(*) FROM orders" against staging is not the one against
  // production, and silently landing on the wrong tab would be a real error.
  test("keeps questions against different sources apart", () => {
    const { result } = renderHook(() => useWorkspace());

    act(() => result.current.openWith({ title: "A", sql: "SELECT 1", connectionId: "c1" }));
    act(() => result.current.openWith({ title: "B", sql: "SELECT 1", connectionId: "c2" }));

    expect(result.current.workspace.tabs).toHaveLength(3);
  });
});
