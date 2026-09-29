import { useCallback, useEffect, useState } from "react";

/**
 * The height of the editor, above the results.
 *
 * Dragged, and remembered. Which of the two halves somebody wants larger is
 * not a thing a product can know: writing a long query and reading a wide
 * result want opposite layouts, and the person doing it switches between them
 * several times an hour. A split that reset on every reload would be a
 * preference expressed and then ignored.
 *
 * Stored in localStorage rather than on the server for the same reason drafts
 * are: it is a property of this screen on this machine, not of the person, and
 * nobody wants their laptop's layout following them to a shared display.
 */

const STORAGE_KEY = "pivot.editor.split.v1";

/** Bounds, so neither half can be dragged away entirely. */
export const MIN_EDITOR_HEIGHT = 120;
export const MAX_EDITOR_HEIGHT = 800;
export const DEFAULT_EDITOR_HEIGHT = 224;

export function clampHeight(height: number): number {
  if (!Number.isFinite(height)) return DEFAULT_EDITOR_HEIGHT;

  return Math.min(MAX_EDITOR_HEIGHT, Math.max(MIN_EDITOR_HEIGHT, Math.round(height)));
}

function load(): number {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_EDITOR_HEIGHT;

    return clampHeight(Number(raw));
  } catch {
    // Private windows, cleared site data, a server render: the split is a
    // convenience and its absence must not stop the page rendering.
    return DEFAULT_EDITOR_HEIGHT;
  }
}

export function useSplit() {
  const [height, setHeight] = useState<number>(load);

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, String(height));
    } catch {
      // A blocked store costs the preference and nothing else.
    }
  }, [height]);

  /*
   * Dragging is tracked on the window rather than the handle.
   *
   * A pointer leaves a six-pixel grip almost immediately -- dragging is a
   * vertical movement across a horizontal strip -- and a listener on the
   * handle would stop following after a few pixels.
   */
  const startDrag = useCallback(
    (event: { clientY: number; preventDefault: () => void }) => {
      event.preventDefault();

      const startY = event.clientY;
      const startHeight = height;

      const onMove = (move: PointerEvent) => {
        setHeight(clampHeight(startHeight + move.clientY - startY));
      };

      const onUp = () => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
      };

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [height],
  );

  /*
   * And by keyboard, because a splitter that only responds to a pointer is a
   * layout somebody using a keyboard cannot change at all. The ARIA separator
   * pattern asks for exactly this.
   */
  const nudge = useCallback((delta: number) => {
    setHeight((current) => clampHeight(current + delta));
  }, []);

  return { height, startDrag, nudge };
}

/** For tests, which must not inherit a split from each other. */
export function clearSplit(): void {
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing to clear.
  }
}
