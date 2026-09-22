import * as TabsPrimitive from "@radix-ui/react-tabs";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";

/**
 * Panels that share a space, one visible at a time.
 *
 * The tab list is a single tab stop and the arrow keys move between tabs —
 * that is the ARIA pattern, and it is why a row of buttons styled to look like
 * tabs is not tabs. Radix supplies it, along with the `aria-controls` and
 * `aria-selected` wiring that ties each tab to its panel.
 *
 * Content stays mounted only when asked (`forceMount` on a panel): an unmounted
 * panel loses its scroll position and any half-typed input, which matters for a
 * query editor and not for a summary.
 */

export const Tabs = TabsPrimitive.Root;

export function TabsList({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn("flex items-center gap-1 border-b border-line", className)}
      {...props}
    />
  );
}

export function TabsTrigger({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        "-mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium",
        "text-content-muted transition-colors",
        "hover:text-content",
        // The selected tab is marked by an underline *and* a color change.
        // Color alone would leave the selection invisible to a color-blind
        // user, and the underline alone reads as weak on a dense page.
        "data-[state=active]:border-accent data-[state=active]:text-content",
        "disabled:pointer-events-none disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export function TabsContent({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content className={cn("py-4 text-sm", className)} {...props} />;
}
