import * as MenuPrimitive from "@radix-ui/react-dropdown-menu";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "../lib/cn";

/**
 * A menu of actions, opened from a button.
 *
 * A menu, not a listbox: the items *do* things. That distinction decides the
 * roles Radix emits and therefore what a screen reader says, and picking the
 * wrong one is how a "choose a value" control ends up announcing itself as a
 * set of commands. Use [Select] when the items choose a value instead.
 *
 * Radix supplies the arrow keys, typeahead, Escape, and returning focus to the
 * trigger on close.
 */

export const DropdownMenu = MenuPrimitive.Root;
export const DropdownMenuTrigger = MenuPrimitive.Trigger;
export const DropdownMenuGroup = MenuPrimitive.Group;

/** The shared surface for a menu and its submenus. */
const SURFACE = [
  "z-50 min-w-40 overflow-hidden rounded-token p-1",
  "border border-line bg-surface-raised text-content shadow-token-lg",
].join(" ");

export function DropdownMenuContent({
  className,
  sideOffset = 6,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.Content>) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Content
        sideOffset={sideOffset}
        className={cn(SURFACE, className)}
        {...props}
      />
    </MenuPrimitive.Portal>
  );
}

/** The shared look of a row. */
const ITEM = [
  "relative flex cursor-default select-none items-center gap-2 rounded-token-sm",
  "px-2 py-1.5 text-sm outline-none",
  // data-highlighted is the keyboard *and* pointer highlight, so arrowing to a
  // row and hovering it look the same. A row highlighted only on :hover is
  // invisible to someone using the arrow keys.
  "data-[highlighted]:bg-accent-subtle data-[highlighted]:text-accent",
  "data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
].join(" ");

export function DropdownMenuItem({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.Item>) {
  return <MenuPrimitive.Item className={cn(ITEM, className)} {...props} />;
}

export function DropdownMenuCheckboxItem({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.CheckboxItem>) {
  return (
    <MenuPrimitive.CheckboxItem className={cn(ITEM, "ps-7", className)} {...props}>
      <span className="absolute start-2 flex size-3.5 items-center justify-center">
        <MenuPrimitive.ItemIndicator>
          <CheckGlyph />
        </MenuPrimitive.ItemIndicator>
      </span>

      {children}
    </MenuPrimitive.CheckboxItem>
  );
}

export const DropdownMenuRadioGroup = MenuPrimitive.RadioGroup;

export function DropdownMenuRadioItem({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.RadioItem>) {
  return (
    <MenuPrimitive.RadioItem className={cn(ITEM, "ps-7", className)} {...props}>
      <span className="absolute start-2 flex size-3.5 items-center justify-center">
        <MenuPrimitive.ItemIndicator>
          <span className="size-1.5 rounded-full bg-current" />
        </MenuPrimitive.ItemIndicator>
      </span>

      {children}
    </MenuPrimitive.RadioItem>
  );
}

export function DropdownMenuLabel({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.Label>) {
  return (
    <MenuPrimitive.Label
      className={cn("px-2 py-1.5 text-xs font-medium text-content-muted", className)}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.Separator>) {
  return (
    <MenuPrimitive.Separator className={cn("-mx-1 my-1 h-px bg-line", className)} {...props} />
  );
}

/** A keyboard shortcut hint. Decorative — the item's label is the name. */
export function DropdownMenuShortcut({ children }: { children: ReactNode }) {
  return (
    <span aria-hidden="true" className="ms-auto text-xs tracking-widest text-content-subtle">
      {children}
    </span>
  );
}

export const DropdownMenuSub = MenuPrimitive.Sub;

export function DropdownMenuSubTrigger({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.SubTrigger>) {
  return (
    <MenuPrimitive.SubTrigger className={cn(ITEM, className)} {...props}>
      {children}

      <svg viewBox="0 0 16 16" aria-hidden="true" className="ms-auto size-3.5 rtl:rotate-180">
        <path
          d="M6 3l5 5-5 5"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </MenuPrimitive.SubTrigger>
  );
}

export function DropdownMenuSubContent({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof MenuPrimitive.SubContent>) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.SubContent className={cn(SURFACE, className)} {...props} />
    </MenuPrimitive.Portal>
  );
}

function CheckGlyph() {
  return (
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
  );
}
