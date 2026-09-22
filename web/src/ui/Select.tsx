import * as SelectPrimitive from "@radix-ui/react-select";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "../lib/cn";
import { useFieldControlProps } from "./Field";

/**
 * A control that picks one value from a list.
 *
 * A listbox, not a menu — the items *are* the value. [DropdownMenu] is the one
 * whose items do things, and the roles Radix emits differ accordingly.
 *
 * Not a native `<select>`, which cannot be styled across browsers and cannot
 * hold the grouped, described options Phase 1's connection and field pickers
 * need. Radix reimplements the behavior that matters: typeahead, Home and
 * End, Escape, the value announced on the trigger, and focus returning there
 * on close.
 */

export const SelectRoot = SelectPrimitive.Root;
export const SelectValue = SelectPrimitive.Value;
export const SelectGroup = SelectPrimitive.Group;

export type SelectTriggerProps = ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger>;

/**
 * The button that opens the list.
 *
 * Must be inside a [Field], which supplies the label and the description. The
 * trigger carries the control's id, so the label points at the thing a click
 * should open.
 */
export function SelectTrigger({ className, children, ...props }: SelectTriggerProps) {
  const fieldProps = useFieldControlProps();

  return (
    <SelectPrimitive.Trigger
      {...fieldProps}
      className={cn(
        "flex h-10 w-full items-center justify-between gap-2",
        "rounded-token border border-line-strong bg-surface px-3",
        "text-start text-sm text-content",
        "data-[placeholder]:text-content-subtle",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "aria-[invalid]:border-danger",
        className,
      )}
      {...props}
    >
      {children}

      <SelectPrimitive.Icon asChild>
        <svg viewBox="0 0 16 16" aria-hidden="true" className="size-4 text-content-muted">
          <path
            d="M4 6l4 4 4-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

export function SelectContent({
  className,
  position = "popper",
  children,
  ...props
}: ComponentPropsWithoutRef<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        position={position}
        sideOffset={6}
        className={cn(
          "z-50 max-h-72 min-w-[var(--radix-select-trigger-width)] overflow-hidden",
          "rounded-token border border-line bg-surface-raised text-content shadow-token-lg",
          className,
        )}
        {...props}
      >
        <SelectPrimitive.ScrollUpButton className="flex justify-center py-1 text-content-muted">
          <Chevron className="rotate-180" />
        </SelectPrimitive.ScrollUpButton>

        {/* The viewport is what scrolls, so the items go inside it rather
            than directly in the content. */}
        <SelectPrimitive.Viewport className="p-1">{children}</SelectPrimitive.Viewport>

        <SelectPrimitive.ScrollDownButton className="flex justify-center py-1 text-content-muted">
          <Chevron />
        </SelectPrimitive.ScrollDownButton>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
}

export function SelectItem({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof SelectPrimitive.Item>) {
  return (
    <SelectPrimitive.Item
      className={cn(
        "relative flex cursor-default select-none items-center rounded-token-sm",
        "py-1.5 pe-2 ps-7 text-sm outline-none",
        "data-[highlighted]:bg-accent-subtle data-[highlighted]:text-accent",
        "data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
        className,
      )}
      {...props}
    >
      <span className="absolute start-2 flex size-3.5 items-center justify-center">
        <SelectPrimitive.ItemIndicator>
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
        </SelectPrimitive.ItemIndicator>
      </span>

      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
    </SelectPrimitive.Item>
  );
}

export function SelectLabel({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof SelectPrimitive.Label>) {
  return (
    <SelectPrimitive.Label
      className={cn("px-2 py-1.5 text-xs font-medium text-content-muted", className)}
      {...props}
    />
  );
}

export function SelectSeparator({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof SelectPrimitive.Separator>) {
  return (
    <SelectPrimitive.Separator className={cn("-mx-1 my-1 h-px bg-line", className)} {...props} />
  );
}

function Chevron({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true" className={cn("size-4", className)}>
      <path
        d="M4 6l4 4 4-4"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
