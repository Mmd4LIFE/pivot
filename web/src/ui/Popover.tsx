import * as PopoverPrimitive from "@radix-ui/react-popover";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";

/**
 * A panel anchored to a control, holding content the user can interact with.
 *
 * The difference from [Tooltip] is not the look, it is the keyboard: a popover
 * takes focus and its contents are reachable by Tab, a tooltip never takes
 * focus and holds nothing interactive. Putting a button inside a tooltip makes
 * it unreachable for everyone not using a mouse, which is why these are two
 * components and not one with a prop.
 */

export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;
export const PopoverAnchor = PopoverPrimitive.Anchor;
export const PopoverClose = PopoverPrimitive.Close;

export function PopoverContent({
  className,
  align = "center",
  sideOffset = 6,
  ...props
}: ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        align={align}
        sideOffset={sideOffset}
        className={cn(
          "z-50 w-72 rounded-token p-4",
          "border border-line bg-surface-raised text-content shadow-token-lg",
          className,
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  );
}
