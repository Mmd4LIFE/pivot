import type {
  HTMLAttributes,
  ReactNode,
  TdHTMLAttributes,
  ThHTMLAttributes,
} from "react";
import { cn } from "../lib/cn";

export interface TableProps extends HTMLAttributes<HTMLTableElement> {
  /**
   * What the table contains, as a sentence.
   *
   * Required. A screen reader user landing on a table hears "table, 7 columns,
   * 200 rows" and nothing else; the caption is the only thing that says which
   * table it is, and on a page with several it is the difference between
   * usable and not.
   */
  caption: ReactNode;

  /** Hide the caption visually. It stays available to assistive technology. */
  captionHidden?: boolean;

  children: ReactNode;
}

/**
 * A data table.
 *
 * A real <table>, not a grid of divs. The semantics — header association, row
 * and column counts, "column 3 of 7" while arrowing across — come from the
 * element and cannot be reproduced with ARIA without reimplementing all of it.
 */
export function Table({
  caption,
  captionHidden = false,
  className,
  children,
  ...props
}: TableProps) {
  return (
    // The wrapper scrolls, and it is focusable so a keyboard user can scroll
    // it. Without tabIndex an overflowing table is reachable by mouse wheel
    // only, which is a genuine wall for anyone not using one.
    <div
      tabIndex={0}
      role="region"
      aria-label={typeof caption === "string" ? caption : undefined}
      className="w-full overflow-x-auto rounded-token border border-line"
    >
      <table
        className={cn("w-full border-collapse text-start text-sm", className)}
        {...props}
      >
        <caption
          className={cn(
            captionHidden ? "sr-only" : "p-3 text-start text-sm text-content-muted",
          )}
        >
          {caption}
        </caption>

        {children}
      </table>
    </div>
  );
}

export function THead({ className, children, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <thead className={cn("bg-surface-sunken", className)} {...props}>
      {children}
    </thead>
  );
}

export function TBody({ className, children, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <tbody className={className} {...props}>
      {children}
    </tbody>
  );
}

export function Tr({ className, children, ...props }: HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr className={cn("border-b border-line last:border-b-0", className)} {...props}>
      {children}
    </tr>
  );
}

export interface ThProps extends ThHTMLAttributes<HTMLTableCellElement> {
  /**
   * Sort state, when the column is sortable.
   *
   * Set on every sortable column, not only the active one: aria-sort="none"
   * is what tells a screen reader the column *can* be sorted. Omitting it from
   * the inactive columns makes the feature invisible to the people who most
   * need to be told it exists.
   */
  sort?: "ascending" | "descending" | "none";

  /** "col" for a column header, "row" for a row header. */
  scope?: "col" | "row";
}

export function Th({ sort, scope = "col", className, children, ...props }: ThProps) {
  return (
    <th
      scope={scope}
      aria-sort={sort}
      className={cn(
        "px-3 py-2 text-start font-medium text-content-muted",
        // Numeric columns are right-aligned by the caller; the default is
        // start-aligned so RTL mirrors without a change.
        className,
      )}
      {...props}
    >
      {children}
    </th>
  );
}

export function Td({ className, children, ...props }: TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td className={cn("px-3 py-2 text-content", className)} {...props}>
      {children}
    </td>
  );
}
