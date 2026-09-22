import * as SeparatorPrimitive from "@radix-ui/react-separator";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";

export type SeparatorProps = ComponentPropsWithoutRef<typeof SeparatorPrimitive.Root>;

/**
 * A dividing line.
 *
 * Decorative by default, which sets aria-hidden: a separator that announces
 * itself adds noise to every list it divides. Pass `decorative={false}` only
 * when the division carries meaning a sighted user gets from the line alone.
 */
export function Separator({
  className,
  orientation = "horizontal",
  decorative = true,
  ...props
}: SeparatorProps) {
  return (
    <SeparatorPrimitive.Root
      orientation={orientation}
      decorative={decorative}
      className={cn(
        "shrink-0 bg-line",
        orientation === "horizontal" ? "h-px w-full" : "h-full w-px",
        className,
      )}
      {...props}
    />
  );
}
