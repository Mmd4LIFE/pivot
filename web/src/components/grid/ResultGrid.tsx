import { useVirtualizer } from "@tanstack/react-virtual";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useTranslation } from "react-i18next";

import { MAX_WIDTH, MIN_WIDTH, compare, isNumeric, kindOf, render } from "./cells";
import { all, at, bounds, contains, move, toTSV, type Direction, type Selection } from "./selection";

/**
 * The results grid.
 *
 * Virtualized, so a hundred thousand rows scroll rather than lock the tab, and
 * selectable the way a database tool is: a cell cursor, shift to extend, drag
 * to sweep, and a copy that pastes into a spreadsheet with its columns intact.
 *
 * Rows are virtualized and columns are not, deliberately. A result has an
 * unbounded number of rows and a bounded number of columns -- fifty is a wide
 * query -- so windowing the columns would add the harder half of the problem
 * (variable widths, horizontal scroll sync, a sticky first column) to buy
 * nothing anybody would notice.
 *
 * It stays a real <table>. The rows outside the window are replaced by two
 * spacer rows holding the missing height, which keeps header association,
 * "column 3 of 7", and every other thing the element gives a screen reader for
 * free -- none of which can be reproduced with ARIA without rebuilding all of
 * it. `aria-rowcount` tells assistive technology the true total, and each real
 * row carries its true `aria-rowindex`.
 */

export interface GridColumn {
  name: string;
  type: string;
  sourceType: string;
}

export interface ResultGridProps {
  columns: GridColumn[];
  rows: unknown[][];

  /** Height of the scrolling area. */
  height?: number;
}

const ROW_HEIGHT = 28;
const ROW_NUMBER_WIDTH = 56;

/*
 * The type row's height, which the name row below it sticks beneath.
 *
 * Fixed rather than measured because `position: sticky` needs a number to
 * offset by. Without it the name row scrolls away and data slides up behind
 * the type row -- which looked like values appearing inside the header.
 */
const TYPE_ROW_HEIGHT = 20;

type Sort = { col: number; direction: "asc" | "desc" } | null;

export function ResultGrid({ columns, rows, height = 420 }: ResultGridProps) {
  const { t } = useTranslation();

  const scroller = useRef<HTMLDivElement | null>(null);
  const [selection, setSelection] = useState<Selection>(() => at(0, 0));
  const [sort, setSort] = useState<Sort>(null);
  const [widths, setWidths] = useState<Record<string, number>>({});
  const sweeping = useRef(false);
  const headers = useRef(new Map<string, HTMLElement>());

  /*
   * The browser sizes the columns, and then they are locked in.
   *
   * Nothing here estimates a text width, and that is the point. Four attempts
   * did -- guessed per-character constants, then a canvas measurement, then a
   * resolved font, then a correction pass -- and every one was wrong, because
   * canvas silently ignores a font string it cannot parse and goes on
   * measuring at 10px sans-serif without saying so. Each failure looked like a
   * number that needed tuning.
   *
   * So the first render carries no widths at all: the table lays out at
   * `max-content` and the browser sizes every column to its heading and its
   * visible values, in the real font, at the real size. Immediately after, the
   * result is read back and fixed, which is what makes columns resizable and
   * stops them resizing themselves as somebody scrolls new values into view.
   *
   * It cannot be wrong about fonts, because it never asks about fonts.
   */
  useLayoutEffect(() => {
    if (Object.keys(widths).length > 0) return;

    const measured: Record<string, number> = {};

    headers.current.forEach((element, name) => {
      const natural = element.getBoundingClientRect().width;

      if (natural > 0) {
        measured[name] = Math.round(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, natural)));
      }
    });

    if (Object.keys(measured).length === columns.length && columns.length > 0) {
      setWidths(measured);
    }
  }, [columns, widths]);

  // A new result is a new shape, so the widths are measured again.
  useEffect(() => {
    setWidths({});
  }, [columns, rows]);

  const sorted = useMemo(() => {
    if (!sort) return rows;

    const numeric = isNumeric(columns[sort.col]?.type ?? "");
    const factor = sort.direction === "asc" ? 1 : -1;

    // A copy, because sorting in place would reorder what the caller handed us
    // and make a second render disagree with the first.
    return [...rows].sort((a, b) => factor * compare(a[sort.col], b[sort.col], numeric));
  }, [rows, sort, columns]);

  const extent = { rows: sorted.length, cols: columns.length };

  const virtualizer = useVirtualizer({
    count: sorted.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,

    /*
       A size to work from before anything has been measured.

       Without it the first paint renders no rows: the scroll container has no
       measured height until layout has run, and a virtualizer told the
       viewport is zero pixels tall correctly concludes that nothing is
       visible. In a browser that is a flash of empty grid; in a test
       environment that never lays anything out it is an empty grid for good.

       The configured height is the right guess because it is what the element
       is about to be.
    */
    initialRect: { width: 1024, height },

    /*
       The observed size, with a floor.

       A scroll container reports zero height until layout has run, and a
       virtualizer told the viewport is zero pixels tall correctly concludes
       that no rows are visible -- so the grid renders empty. In a browser that
       is a flash before the first frame; in an environment that never lays
       anything out it is permanent, which is how this was found.

       Treating a zero measurement as the configured height is honest rather
       than a workaround: that is the height the element has been given and is
       about to have. A real measurement always wins.
    */
    observeElementRect: (instance, report) => {
      const element = instance.scrollElement;
      if (!element) return undefined;

      const measure = () => {
        const rect = element.getBoundingClientRect();

        report({ width: rect.width || 1024, height: rect.height || height });
      };

      measure();

      const observer = new ResizeObserver(measure);
      observer.observe(element);

      return () => observer.disconnect();
    },
  });

  const items = virtualizer.getVirtualItems();
  const before = items.length > 0 ? (items[0]?.start ?? 0) : 0;
  const after =
    items.length > 0 ? virtualizer.getTotalSize() - (items[items.length - 1]?.end ?? 0) : 0;

  const copy = useCallback(() => {
    const text = toTSV(selection, sorted, (value, col) => render(value, columns[col]?.type));

    // The clipboard API is unavailable over plain HTTP on a non-localhost
    // origin, which is a real way somebody runs this. Failing quietly there is
    // better than an exception in the console nobody sees.
    void navigator.clipboard?.writeText(text).catch(() => {});
  }, [columns, selection, sorted]);

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      const directions: Record<string, Direction> = {
        ArrowUp: "up",
        ArrowDown: "down",
        ArrowLeft: "left",
        ArrowRight: "right",
      };

      const meta = event.ctrlKey || event.metaKey;

      if (meta && event.key.toLowerCase() === "c") {
        event.preventDefault();
        copy();

        return;
      }

      if (meta && event.key.toLowerCase() === "a") {
        event.preventDefault();
        setSelection(all(extent));

        return;
      }

      const direction = directions[event.key];

      if (direction) {
        event.preventDefault();

        setSelection((current) => {
          const focus = move(current.focus, direction, extent);

          // Shift extends from the anchor; anything else starts afresh, which
          // is what every grid does and what fingers expect.
          return event.shiftKey ? { anchor: current.anchor, focus } : at(focus.row, focus.col);
        });

        return;
      }

      const jumps: Record<string, () => Selection> = {
        Home: () => at(selection.focus.row, 0),
        End: () => at(selection.focus.row, extent.cols - 1),
        PageDown: () => at(Math.min(extent.rows - 1, selection.focus.row + 20), selection.focus.col),
        PageUp: () => at(Math.max(0, selection.focus.row - 20), selection.focus.col),
      };

      const jump = jumps[event.key];

      if (jump) {
        event.preventDefault();
        setSelection(jump());
      }
    },
    [copy, extent, selection.focus.col, selection.focus.row],
  );

  // Keep the focused cell on screen when the keyboard moved it there.
  useEffect(() => {
    virtualizer.scrollToIndex(selection.focus.row, { align: "auto" });
  }, [selection.focus.row, virtualizer]);

  // A sweep that ends anywhere -- including outside the grid -- must stop
  // selecting. Listening on the window rather than the grid is the difference
  // between that and a selection that keeps following the pointer.
  useEffect(() => {
    const stop = () => {
      sweeping.current = false;
    };

    window.addEventListener("pointerup", stop);

    return () => window.removeEventListener("pointerup", stop);
  }, []);

  function onCellPointerDown(event: ReactPointerEvent, row: number, col: number) {
    sweeping.current = true;

    setSelection((current) =>
      event.shiftKey ? { anchor: current.anchor, focus: { row, col } } : at(row, col),
    );
  }

  function onCellPointerEnter(row: number, col: number) {
    if (!sweeping.current) return;

    setSelection((current) => ({ anchor: current.anchor, focus: { row, col } }));
  }

  const selectedBounds = bounds(selection);

  return (
    <div className="flex flex-col gap-2">
      <div
        ref={scroller}
        role="region"
        aria-label={t("grid.label")}
        tabIndex={0}
        onKeyDown={onKeyDown}
        style={{ height }}
        className="overflow-auto rounded-token border border-line bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        <table
          className="border-separate border-spacing-0 text-sm"
          aria-rowcount={sorted.length}
          style={{ width: "max-content" }}
        >
          <caption className="sr-only">
            {t("grid.caption", { rows: sorted.length, columns: columns.length })}
          </caption>

          {/*
            Two header rows: the source's type above, the column's name below.

            Stacked rather than side by side because sharing one row made them
            compete for width, and the name lost -- `customer_id` showed as
            `custome…` over a column of two-digit numbers. Stacked, a column is
            as wide as the widest of its name, its type and its values.

            The type is first because it is the subordinate fact. Reading down
            a column somebody wants the name; the type is there for the moment
            they are wondering why a comparison behaved oddly.
          */}
          <thead>
            <tr>
              <th
                scope="col"
                rowSpan={2}
                style={{ width: ROW_NUMBER_WIDTH, top: 0 }}
                className="sticky left-0 z-40 border-b border-r border-line bg-surface-sunken px-2 align-bottom text-end text-xs font-medium text-content-subtle"
              >
                #
              </th>

              {columns.map((column, index) => (
                <th
                  key={column.name + index + "-type"}
                  aria-hidden="true"
                  style={{
                    ...(widths[column.name] ? { width: widths[column.name] } : {}),
                    top: 0,
                    height: TYPE_ROW_HEIGHT,
                  }}
                  className={`sticky z-30 border-r border-line px-2 pt-1 text-start text-[10px] font-normal uppercase leading-none tracking-wide text-content-subtle ${
                    index >= selectedBounds.left && index <= selectedBounds.right
                      ? "bg-accent/25"
                      : "bg-surface-sunken"
                  }`}
                >
                  <span className="block truncate">{column.sourceType || column.type}</span>
                </th>
              ))}
            </tr>

            <tr>
              {columns.map((column, index) => (
                <HeaderCell
                  key={column.name + index}
                  column={column}
                  width={widths[column.name]}
                  sorted={sort?.col === index ? sort.direction : undefined}
                  selected={index >= selectedBounds.left && index <= selectedBounds.right}
                  /*
                    Ascending, descending, then back to the order the source
                    returned. Three states rather than two, because a sort is
                    something somebody tries and then wants to undo -- and
                    without a way back, "how was this ordered originally" means
                    running the query again.
                  */
                  onSort={() =>
                    setSort((current) => {
                      if (current?.col !== index) return { col: index, direction: "asc" };
                      if (current.direction === "asc") return { col: index, direction: "desc" };

                      return null;
                    })
                  }
                  onResize={(width) =>
                    setWidths((current) => ({ ...current, [column.name]: width }))
                  }
                  // Clearing every width sends the table back through the
                  // browser's own sizing, which is the only measurement that
                  // has ever been right here.
                  onAutoSize={() => setWidths({})}
                  register={(element) => {
                    if (element) headers.current.set(column.name, element);
                    else headers.current.delete(column.name);
                  }}
                />
              ))}
            </tr>
          </thead>

          <tbody>
            {before > 0 ? (
              <tr aria-hidden="true">
                <td colSpan={columns.length + 1} style={{ height: before }} />
              </tr>
            ) : null}

            {items.map((item) => {
              const row = sorted[item.index] ?? [];
              const isSelectedRow =
                item.index >= selectedBounds.top && item.index <= selectedBounds.bottom;

              return (
                <tr key={item.key} aria-rowindex={item.index + 1} style={{ height: ROW_HEIGHT }}>
                  <th
                    scope="row"
                    className={`sticky left-0 z-20 border-b border-r border-line px-2 text-end align-middle font-mono text-xs font-normal tabular-nums ${
                      isSelectedRow
                        ? "bg-accent/25 text-content"
                        : "bg-surface-sunken text-content-subtle"
                    }`}
                  >
                    {item.index + 1}
                  </th>

                  {columns.map((column, col) => (
                    <GridCell
                      key={column.name + col}
                      value={row[col]}
                      kind={column.type}
                      numeric={isNumeric(column.type)}
                      selected={contains(selection, item.index, col)}
                      focused={selection.focus.row === item.index && selection.focus.col === col}
                      onPointerDown={(event) => onCellPointerDown(event, item.index, col)}
                      onPointerEnter={() => onCellPointerEnter(item.index, col)}
                    />
                  ))}
                </tr>
              );
            })}

            {after > 0 ? (
              <tr aria-hidden="true">
                <td colSpan={columns.length + 1} style={{ height: after }} />
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <GridStatus selection={selection} rows={sorted} onCopy={copy} />
    </div>
  );
}

function HeaderCell({
  column,
  width,
  sorted,
  selected,
  onSort,
  onResize,
  onAutoSize,
  register,
}: {
  column: GridColumn;
  width: number | undefined;
  sorted: "asc" | "desc" | undefined;
  selected: boolean;
  onSort: () => void;
  onResize: (width: number) => void;
  onAutoSize: () => void;

  /** Hands the name element back, so the grid can see whether it fitted. */
  register: (element: HTMLElement | null) => void;
}) {
  const { t } = useTranslation();

  /*
   * Resizing tracked on the window, not the handle.
   *
   * A pointer that leaves the 6px handle mid-drag -- which it does immediately,
   * because dragging is a horizontal movement across a vertical strip -- would
   * otherwise stop resizing after a few pixels.
   */
  function startResize(event: ReactPointerEvent) {
    event.preventDefault();
    event.stopPropagation();

    const startX = event.clientX;
    const startWidth = width ?? 140;

    const onMove = (moveEvent: PointerEvent) => {
      onResize(Math.max(56, startWidth + moveEvent.clientX - startX));
    };

    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };

    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

  return (
    <th
      scope="col"
      aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"}
      /*
        Sticky beneath the type row, so both halves of the header stay put.
        Applied to the cell rather than to <thead>, which is not a sticky
        container in every engine -- with it on the thead alone the name row
        scrolled away and data appeared to slide up inside the header.
      */
      style={{
        ...(width ? { width, minWidth: width, maxWidth: width } : {}),
        top: TYPE_ROW_HEIGHT,
      }}
      className={`group sticky z-30 border-b border-r border-line px-0 py-0 text-start align-middle ${
        selected ? "bg-accent/25" : "bg-surface-sunken"
      }`}
    >
      <button
        type="button"
        onClick={onSort}
        title={`${column.name} — ${column.sourceType || column.type}`}
        className="flex w-full items-baseline gap-1 overflow-hidden px-2 pb-1.5 text-start"
      >
        <span ref={register} className="truncate font-medium text-content">
          {column.name}
        </span>

        <span aria-hidden="true" className="ml-auto shrink-0 text-content-muted">
          {sorted === "asc" ? "▲" : sorted === "desc" ? "▼" : ""}
        </span>
      </button>

      <span
        role="separator"
        aria-orientation="vertical"
        aria-label={t("grid.resize", { column: column.name })}
        onPointerDown={startResize}
        onDoubleClick={onAutoSize}
        className="absolute right-0 top-0 h-full w-1.5 cursor-col-resize bg-transparent hover:bg-accent"
      />
    </th>
  );
}

function GridCell({
  value,
  kind: typeKind,
  numeric,
  selected,
  focused,
  onPointerDown,
  onPointerEnter,
}: {
  value: unknown;
  kind: string;
  numeric: boolean;
  selected: boolean;
  focused: boolean;
  onPointerDown: (event: ReactPointerEvent) => void;
  onPointerEnter: () => void;
}) {
  const kind = kindOf(value);
  const text = render(value, typeKind);

  return (
    <td
      onPointerDown={onPointerDown}
      onPointerEnter={onPointerEnter}
      title={text}
      aria-selected={selected}
      className={[
        "select-none overflow-hidden text-ellipsis whitespace-nowrap border-b border-r border-line px-2 align-middle",
        numeric ? "text-end font-mono tabular-nums" : "text-start",
        // NULL and an empty string are three-way distinct from text, because
        // "is this missing or blank" is a question people ask constantly and
        // an empty cell cannot answer it.
        kind === "null" ? "italic text-content-subtle" : "",
        kind === "empty" ? "text-content-subtle" : "",
        /*
          The range, at an alpha over the accent rather than the
          `accent-subtle` token.

          That token is 21 29 44 in dark against a surface of 24 24 27 -- a
          three-value difference, which is invisible. A wash of the accent
          reads clearly in both themes and still leaves the text above it
          legible, which a solid fill would not.
        */
        selected ? "bg-accent/25" : "",

        /*
          The focused cell, ringed rather than filled, so it is findable
          *inside* a large selection. Filling it would make the one cell
          somebody is about to type over the hardest one to see.
        */
        focused ? "bg-accent/40 outline outline-2 -outline-offset-2 outline-accent" : "",
      ].join(" ")}
    >
      {kind === "empty" ? <span aria-label="empty string">⌀</span> : text}
    </td>
  );
}

function GridStatus({
  selection,
  rows,
  onCopy,
}: {
  selection: Selection;
  rows: unknown[][];
  onCopy: () => void;
}) {
  const { t } = useTranslation();
  const b = bounds(selection);

  const cells = (b.bottom - b.top + 1) * (b.right - b.left + 1);

  /*
   * A sum over the selection, when it is all numbers.
   *
   * The one thing people do in a spreadsheet more than anything else is select
   * a column and look at the bottom of the window. Offering it here is the
   * difference between reading a result and exporting it to find out.
   */
  const sum = useMemo(() => {
    let total = 0;
    let numbers = 0;

    for (let row = b.top; row <= b.bottom; row++) {
      for (let col = b.left; col <= b.right; col++) {
        const value = rows[row]?.[col];
        const n = typeof value === "number" ? value : Number(value);

        if (value !== null && value !== "" && !Number.isNaN(n)) {
          total += n;
          numbers++;
        }
      }
    }

    return numbers > 0 && numbers === cells ? total : undefined;
  }, [b.bottom, b.left, b.right, b.top, cells, rows]);

  return (
    <div className="flex flex-wrap items-center gap-3 px-1 text-xs text-content-muted">
      <span>{t("grid.selected", { count: cells })}</span>

      {sum !== undefined ? (
        <span className="font-mono tabular-nums">
          {t("grid.sum")} {Math.round(sum * 1e6) / 1e6}
        </span>
      ) : null}

      <button
        type="button"
        onClick={onCopy}
        className="rounded-token-sm px-2 py-0.5 underline-offset-2 hover:bg-surface-sunken hover:underline"
      >
        {t("grid.copy")}
      </button>
    </div>
  );
}

export default ResultGrid;
