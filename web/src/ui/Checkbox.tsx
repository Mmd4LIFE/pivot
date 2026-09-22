import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";

export type CheckboxProps = ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root> & {
  /** The visible label. Required: a checkbox without one cannot be used. */
  label: string;
};

/**
 * A checkbox.
 *
 * Radix supplies the keyboard behavior and the tristate ARIA; what is here is
 * the styling and the label association. The label is a required prop rather
 * than optional children, because an unlabeled checkbox is the single most
 * common accessibility failure in a form and making it impossible is cheaper
 * than catching it in review.
 */
export function Checkbox({ className, label, id, ...props }: CheckboxProps) {
  const inputId = id ?? `checkbox-${label.replace(/\s+/g, "-").toLowerCase()}`;

  return (
    <div className="flex items-center gap-2">
      <CheckboxPrimitive.Root
        id={inputId}
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-token-sm",
          "border border-line-strong bg-surface",
          "data-[state=checked]:border-accent data-[state=checked]:bg-accent",
          "data-[state=indeterminate]:border-accent data-[state=indeterminate]:bg-accent",
          "disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
        {...props}
      >
        <CheckboxPrimitive.Indicator className="text-accent-content">
          <svg viewBox="0 0 16 16" aria-hidden="true" className="size-3.5">
            <path
              d="M3 8.5l3 3 7-7"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </CheckboxPrimitive.Indicator>
      </CheckboxPrimitive.Root>

      <label htmlFor={inputId} className="text-sm text-content select-none">
        {label}
      </label>
    </div>
  );
}
