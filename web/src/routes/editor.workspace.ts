import { useCallback, useEffect, useState } from "react";

/**
 * The editor's tabs, and the reason they survive a reload.
 *
 * Losing a half-written query to a refresh is the thing people never forgive,
 * and it is the kind of loss that makes somebody keep a text file open beside
 * the tool instead of using it. So the workspace is written to localStorage on
 * every change.
 *
 * localStorage rather than the server, deliberately. A draft is not content:
 * it is not shared, not versioned, and not something anybody wants
 * synchronised across devices mid-sentence. Saved queries are Phase 2's, and
 * they are a different thing with a different lifecycle -- conflating them
 * would mean every keystroke became a write somebody else could see.
 *
 * Every read and write is guarded. Storage throws in a private window, comes
 * back empty when site data is cleared, and is absent during a server render;
 * a workspace that failed to load must leave somebody a usable editor rather
 * than a blank screen.
 */

const STORAGE_KEY = "pivot.editor.workspace.v1";

export interface EditorTab {
  id: string;
  title: string;
  sql: string;
  connectionId: string;
}

export interface Workspace {
  tabs: EditorTab[];
  activeId: string;
}

/*
 * The workspace somebody gets on a first visit, or when the stored one cannot
 * be read.
 *
 * The first tab starts with a statement that runs. An empty editor with a
 * disabled Run button is a dead end on the one screen where somebody is trying
 * to find out whether any of this works -- and `SELECT 1` works against every
 * source Pivot speaks to, which is exactly the point of it.
 *
 * Tabs opened afterwards start empty, because by then the question has been
 * answered and somebody is about to type something of their own.
 */
export function emptyWorkspace(): Workspace {
  const tab = { ...newTab(1), sql: FIRST_STATEMENT };

  return { tabs: [tab], activeId: tab.id };
}

/** Runs everywhere, proves the round trip, and is short enough to replace. */
const FIRST_STATEMENT = "SELECT 1";

let counter = 0;

/**
 * A tab id that is unique within this page.
 *
 * A counter rather than a random id or a timestamp: the tests need it to be
 * predictable, and nothing outside this page ever sees it.
 */
function newTab(n: number): EditorTab {
  counter += 1;

  return { id: `tab-${counter}`, title: `Query ${n}`, sql: "", connectionId: "" };
}

function load(): Workspace {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return emptyWorkspace();

    const parsed = JSON.parse(raw) as Partial<Workspace>;

    // Validated rather than trusted. This is data a previous version of Pivot
    // wrote, and a shape change must not present as a crash on the page
    // somebody was in the middle of using.
    if (!Array.isArray(parsed.tabs) || parsed.tabs.length === 0) return emptyWorkspace();

    const tabs = parsed.tabs.filter(
      (tab): tab is EditorTab =>
        typeof tab?.id === "string" &&
        typeof tab?.sql === "string" &&
        typeof tab?.title === "string",
    );

    if (tabs.length === 0) return emptyWorkspace();

    // Keep the counter ahead of what was restored, so a new tab cannot take a
    // restored one's id.
    counter = Math.max(counter, tabs.length);

    const activeId = tabs.some((tab) => tab.id === parsed.activeId)
      ? (parsed.activeId as string)
      : tabs[0]!.id;

    return { tabs, activeId };
  } catch {
    return emptyWorkspace();
  }
}

function save(workspace: Workspace): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(workspace));
  } catch {
    // A full or blocked store costs the reload guarantee and nothing else.
    // Refusing to let somebody type because their draft cannot be saved would
    // be the worse trade by a distance.
  }
}

export function useWorkspace() {
  const [workspace, setWorkspace] = useState<Workspace>(load);

  useEffect(() => {
    save(workspace);
  }, [workspace]);

  const active = workspace.tabs.find((tab) => tab.id === workspace.activeId) ?? workspace.tabs[0]!;

  const update = useCallback((changes: Partial<EditorTab>) => {
    setWorkspace((current) => ({
      ...current,
      tabs: current.tabs.map((tab) =>
        tab.id === current.activeId ? { ...tab, ...changes } : tab,
      ),
    }));
  }, []);

  const open = useCallback(() => {
    setWorkspace((current) => {
      const tab = newTab(current.tabs.length + 1);

      return { tabs: [...current.tabs, tab], activeId: tab.id };
    });
  }, []);

  /*
   * Closing the last tab opens an empty one rather than leaving none.
   *
   * A workspace with no tabs has nowhere to type, and the state that produces
   * it is reachable in one click. Replacing it is what somebody meant by
   * closing their only tab anyway.
   */
  const close = useCallback((id: string) => {
    setWorkspace((current) => {
      const tabs = current.tabs.filter((tab) => tab.id !== id);

      if (tabs.length === 0) return emptyWorkspace();

      const activeId = current.activeId === id ? tabs[0]!.id : current.activeId;

      return { tabs, activeId };
    });
  }, []);

  /*
   * Open a tab somebody arrived with -- from a shared link, or from Browse.
   *
   * A new tab rather than a replacement: whatever was already being written is
   * somebody's work, and a link that silently overwrote it would be the last
   * link they clicked.
   */
  const openWith = useCallback((tab: Omit<EditorTab, "id">) => {
    setWorkspace((current) => {
      /*
       * Reuse a tab that already holds this question rather than always
       * appending one.
       *
       * The address bar carries the statement as it is typed, so a reload
       * reads the page's own fragment back on mount. Appending unconditionally
       * meant every refresh opened a second copy of the tab you were already
       * in -- and the one after that a third.
       *
       * Matched on the statement and the source, not the title: a title is the
       * one part of a question somebody renames.
       */
      const existing = current.tabs.find(
        (open) => open.sql === tab.sql && open.connectionId === tab.connectionId,
      );

      if (existing) return { ...current, activeId: existing.id };

      counter += 1;

      const opened: EditorTab = { id: `tab-${counter}`, ...tab };

      return { tabs: [...current.tabs, opened], activeId: opened.id };
    });
  }, []);

  const select = useCallback((id: string) => {
    setWorkspace((current) => ({ ...current, activeId: id }));
  }, []);

  /*
   * Rename a tab.
   *
   * An empty name falls back to the generated one rather than leaving a tab
   * with no label -- a strip of unlabelled tabs is unusable, and clearing the
   * field is what somebody does on the way to typing something else.
   */
  const rename = useCallback((id: string, title: string) => {
    setWorkspace((current) => ({
      ...current,
      tabs: current.tabs.map((tab, index) =>
        tab.id === id ? { ...tab, title: title.trim() || `Query ${index + 1}` } : tab,
      ),
    }));
  }, []);

  /*
   * Move a tab one place left or right.
   *
   * Buttons rather than drag-and-drop, and that is the deliberate part: a
   * reorder somebody can reach with a keyboard is worth more than one that
   * looks better with a mouse, and drag-and-drop without a keyboard equivalent
   * is a feature that excludes people. Drag can be added on top later; it
   * cannot be retrofitted underneath.
   */
  const move = useCallback((id: string, direction: -1 | 1) => {
    setWorkspace((current) => {
      const from = current.tabs.findIndex((tab) => tab.id === id);
      if (from < 0) return current;

      const to = from + direction;
      if (to < 0 || to >= current.tabs.length) return current;

      const tabs = [...current.tabs];
      const [moved] = tabs.splice(from, 1);

      if (moved) tabs.splice(to, 0, moved);

      return { ...current, tabs };
    });
  }, []);

  return { workspace, active, update, open, close, select, openWith, rename, move };
}

/**
 * Put a statement in a new tab, for somebody arriving from somewhere else.
 *
 * Written straight to storage rather than passed through the URL: a statement
 * is long, contains quotes and newlines, and a query string is a poor place to
 * carry one -- it ends up encoded, truncated by something, and visible in
 * every log between here and the server.
 *
 * The editor reads the workspace on mount, so navigating there afterwards
 * lands on the new tab with nothing else to coordinate.
 */
export function openInWorkspace(title: string, sql: string, connectionId: string): void {
  const current = load();

  counter += 1;

  const tab: EditorTab = { id: `tab-${counter}`, title, sql, connectionId };

  save({ tabs: [...current.tabs, tab], activeId: tab.id });
}

/** For tests, which must not inherit a workspace from each other. */
export function clearWorkspace(): void {
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing to clear if storage is unavailable.
  }

  counter = 0;
}
