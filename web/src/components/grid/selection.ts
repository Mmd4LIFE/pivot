/**
 * A rectangular cell selection, the way a spreadsheet means it.
 *
 * Two cells: the anchor, which is where the selection started and does not
 * move, and the focus, which is where the cursor is now. Every other way of
 * modelling this -- a list of selected cells, a set of ranges -- is more
 * general than anything the grid offers and costs the one property that makes
 * keyboard selection feel right: shifting back towards the anchor *shrinks*
 * the selection rather than starting a second one.
 *
 * Kept apart from the component because this is the part with the edge cases,
 * and edge cases are worth testing without a DOM.
 */

export interface Cell {
  row: number;
  col: number;
}

export interface Selection {
  anchor: Cell;
  focus: Cell;
}

export interface Bounds {
  top: number;
  left: number;
  bottom: number;
  right: number;
}

/** The rectangle a selection covers, normalized so top <= bottom. */
export function bounds(selection: Selection): Bounds {
  return {
    top: Math.min(selection.anchor.row, selection.focus.row),
    bottom: Math.max(selection.anchor.row, selection.focus.row),
    left: Math.min(selection.anchor.col, selection.focus.col),
    right: Math.max(selection.anchor.col, selection.focus.col),
  };
}

export function contains(selection: Selection, row: number, col: number): boolean {
  const b = bounds(selection);

  return row >= b.top && row <= b.bottom && col >= b.left && col <= b.right;
}

/** A selection of exactly one cell. */
export function at(row: number, col: number): Selection {
  return { anchor: { row, col }, focus: { row, col } };
}

export type Direction = "up" | "down" | "left" | "right";

export interface Extent {
  rows: number;
  cols: number;
}

/**
 * Move the focus, clamped to the grid.
 *
 * Clamped rather than wrapped. Arrowing off the last row in a spreadsheet
 * stops; it does not jump to the first, and a grid that wrapped would move
 * somebody a thousand rows away from what they were reading with one keypress
 * they did not mean.
 */
export function move(cell: Cell, direction: Direction, extent: Extent): Cell {
  const delta: Record<Direction, [number, number]> = {
    up: [-1, 0],
    down: [1, 0],
    left: [0, -1],
    right: [0, 1],
  };

  const [dr, dc] = delta[direction];

  return {
    row: clamp(cell.row + dr, 0, extent.rows - 1),
    col: clamp(cell.col + dc, 0, extent.cols - 1),
  };
}

export function clamp(n: number, low: number, high: number): number {
  if (high < low) return low;

  return Math.min(Math.max(n, low), high);
}

/** Everything, for Ctrl-A. */
export function all(extent: Extent): Selection {
  return {
    anchor: { row: 0, col: 0 },
    focus: { row: Math.max(0, extent.rows - 1), col: Math.max(0, extent.cols - 1) },
  };
}

/**
 * The selection rendered as TSV.
 *
 * Tab-separated rather than CSV, and this is the decision that makes copy
 * worth having: a spreadsheet pasting TSV puts each value in its own cell
 * without a dialog, while CSV either opens an import wizard or lands in one
 * column. It is what every database tool copies, and it is what people expect
 * to be able to do.
 *
 * A tab or newline *inside* a value would break the alignment, so they are
 * turned into spaces. Quoting would be more faithful and is the wrong trade
 * here: a quoted field pasted into a spreadsheet arrives with its quotes
 * visible, which is a worse answer than a value whose internal tab became a
 * space.
 */
export function toTSV(
  selection: Selection,
  rows: readonly (readonly unknown[])[],
  render: (value: unknown, col: number) => string,
): string {
  const b = bounds(selection);
  const out: string[] = [];

  for (let row = b.top; row <= b.bottom; row++) {
    const cells: string[] = [];

    for (let col = b.left; col <= b.right; col++) {
      cells.push(flatten(render(rows[row]?.[col], col)));
    }

    out.push(cells.join("\t"));
  }

  return out.join("\n");
}

function flatten(value: string): string {
  return value.replace(/[\t\r\n]+/g, " ");
}
