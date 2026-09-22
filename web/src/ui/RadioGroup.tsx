import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";

export type RadioGroupProps = ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Root>;

/**
 * A group of mutually exclusive options.
 *
 * Radix gives this roving tabindex: the group is one tab stop and the arrow
 * keys move between options, which is what the ARIA pattern requires and what
 * hand-rolled radio groups almost always get wrong.
 */
export function RadioGroup({ className, ...props }: RadioGroupProps) {
  return (
    <RadioGroupPrimitive.Root
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  );
}

export type RadioProps = ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Item> & {
  label: string;
};

export function Radio({ className, label, id, value, ...props }: RadioProps) {
  const inputId = id ?? `radio-${value}`;

  return (
    <div className="flex items-center gap-2">
      <RadioGroupPrimitive.Item
        id={inputId}
        value={value}
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-full",
          "border border-line-strong bg-surface",
          "data-[state=checked]:border-accent",
          "disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
        {...props}
      >
        <RadioGroupPrimitive.Indicator className="size-2.5 rounded-full bg-accent" />
      </RadioGroupPrimitive.Item>

      <label htmlFor={inputId} className="text-sm text-content select-none">
        {label}
      </label>
    </div>
  );
}
