import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "../lib/cn";

/**
 * A short label that appears on hover or focus.
 *
 * Two rules, and both are about people who are not using a mouse.
 *
 * Nothing interactive goes inside. A tooltip never takes focus, so a button in
 * one is unreachable by keyboard. Use a [Popover] for that.
 *
 * A tooltip is never the only way to learn something. It does not appear on a
 * touch device at all, and its trigger must make sense without it — which for
 * an icon button means a real accessible name, not a tooltip standing in for
 * one.
 */

export const TooltipProvider = TooltipPrimitive.Provider;

export interface TooltipProps {
  /** The text. Kept short: a tooltip is a label, not documentation. */
  content: ReactNode;

  /** The control it describes. */
  children: ReactNode;

  side?: ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>["side"];

  /** Keep it open. For stories and for debugging placement. */
  defaultOpen?: boolean;
}

export function Tooltip({ content, children, side = "top", defaultOpen = false }: TooltipProps) {
  return (
    <TooltipPrimitive.Root defaultOpen={defaultOpen}>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>

      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          sideOffset={6}
          className={cn(
            "z-50 max-w-64 rounded-token-sm px-2 py-1 text-xs",
            // The inverted pair: dark fill in the light theme, light fill in
            // the dark one. TestTokenContrast holds it to 4.5:1 both ways.
            "bg-content text-content-inverted shadow-token",
          )}
        >
          {content}
          <TooltipPrimitive.Arrow className="fill-content" />
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}
