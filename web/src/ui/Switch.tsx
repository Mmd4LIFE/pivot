import * as SwitchPrimitive from "@radix-ui/react-switch";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";

export type SwitchProps = ComponentPropsWithoutRef<typeof SwitchPrimitive.Root> & {
  label: string;
};

/**
 * An on/off toggle that takes effect immediately.
 *
 * Not a checkbox: a checkbox states an intention that a later Save commits, a
 * switch performs the action now. Using the wrong one is a real usability bug
 * rather than a stylistic one, because it changes whether the user expects to
 * have to confirm.
 */
export function Switch({ className, label, id, ...props }: SwitchProps) {
  const inputId = id ?? `switch-${label.replace(/\s+/g, "-").toLowerCase()}`;

  return (
    <div className="flex items-center gap-2">
      <SwitchPrimitive.Root
        id={inputId}
        className={cn(
          "relative h-6 w-11 shrink-0 rounded-full transition-colors",
          "bg-line-strong data-[state=checked]:bg-accent",
          "disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
        {...props}
      >
        <SwitchPrimitive.Thumb
          className={cn(
            "block size-5 rounded-full bg-surface shadow-token-sm transition-transform",
            // translate-x rather than a logical property: this is a physical
            // movement that must mirror in RTL, and Tailwind emits the RTL
            // variant from the same class.
            "translate-x-0.5 data-[state=checked]:translate-x-[1.375rem]",
            "rtl:-translate-x-0.5 rtl:data-[state=checked]:-translate-x-[1.375rem]",
          )}
        />
      </SwitchPrimitive.Root>

      <label htmlFor={inputId} className="text-sm text-content select-none">
        {label}
      </label>
    </div>
  );
}
