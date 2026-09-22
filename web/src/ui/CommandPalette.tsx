import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandRoot,
  CommandSeparator,
} from "cmdk";
import { useEffect, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";

/**
 * Search-and-run, opened from anywhere with a keystroke.
 *
 * Built on cmdk rather than hand-rolled, for the same reason the rest of this
 * directory is built on Radix: this is a combobox, the combobox pattern is one
 * of the easiest in ARIA to get subtly wrong, and `aria-activedescendant` with
 * a filtered list is precisely the case where "subtly wrong" means a screen
 * reader reads nothing as the user arrows down.
 *
 * It is a shortcut, never the only route. Everything reachable here is also
 * reachable by navigating, because a user who does not know the palette exists
 * must still be able to use the product — and because a palette is useless on
 * a touch device.
 */

export const CommandGroupItems = CommandGroup;
export const CommandDivider = CommandSeparator;

export interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;

  /** Announced when the palette opens. Not shown. */
  label?: string;

  placeholder?: string;

  /**
   * What to show when nothing matches.
   *
   * A prop rather than a child, and rendered *outside* the list, because the
   * list is a `role="listbox"` and a listbox whose only child is a paragraph
   * violates the ARIA contract — axe fails it as `aria-required-children`, and
   * it is a real defect: a screen reader announces a listbox and then finds
   * nothing listable in it. Keeping the structure out of the caller's hands is
   * the only way to be sure it stays right.
   */
  empty?: ReactNode;

  children: ReactNode;
}

export function CommandPalette({
  open,
  onOpenChange,
  label = "Search commands",
  placeholder = "Search...",
  empty = "No results.",
  children,
}: CommandPaletteProps) {
  // Callback refs held in state rather than useRef, because Radix's portal
  // mounts the dialog's contents in a later commit: a ref object would still
  // be null when an effect keyed on `open` ran, and would never be looked at
  // again. As state, the effect re-runs the moment the elements exist.
  const [input, setInput] = useState<HTMLInputElement | null>(null);
  const [list, setList] = useState<HTMLDivElement | null>(null);

  useActiveDescendant(input, list);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-overlay/50" />

        <DialogPrimitive.Content
          className={cn(
            "fixed left-1/2 top-24 z-50 w-[calc(100vw-2rem)] max-w-xl -translate-x-1/2",
            "overflow-hidden rounded-token border border-line",
            "bg-surface-raised text-content shadow-token-lg",
          )}
        >
          {/*
            A dialog must have a title. This one has no room for a visible one,
            so it is announced and not drawn -- which is what sr-only is for,
            and is not the same as leaving it out.
          */}
          <DialogPrimitive.Title className="sr-only">{label}</DialogPrimitive.Title>

          <CommandRoot
            // cmdk's own filtering, so typing narrows the list without a
            // round trip. Phase 1 will want server-side results too; that is a
            // `shouldFilter={false}` away and not a rewrite.
            label={label}
            className="flex max-h-96 flex-col"
          >
            {/*
              The ring is on the row rather than the input, because the input
              has no border of its own and an outline drawn tight around it
              looks like a rendering fault. Something still has to show where
              focus is -- this is the only focusable control in the palette.
            */}
            <div
              className={cn(
                "flex items-center gap-2 border-b border-line px-3",
                "focus-within:border-accent",
              )}
            >
              <svg viewBox="0 0 16 16" aria-hidden="true" className="size-4 text-content-muted">
                <path
                  d="M7 12a5 5 0 1 0 0-10 5 5 0 0 0 0 10zm3.5.5L14 16"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.75"
                  strokeLinecap="round"
                />
              </svg>

              <CommandInput
                ref={setInput}
                placeholder={placeholder}
                className={cn(
                  "h-11 w-full bg-transparent text-sm text-content outline-none",
                  "placeholder:text-content-subtle",
                )}
              />
            </div>

            <CommandList ref={setList} className="overflow-y-auto p-1">
              {children}
            </CommandList>

            {/* Outside the listbox, for the reason on the `empty` prop. */}
            <CommandEmpty className="p-4 text-center text-sm text-content-muted">
              {empty}
            </CommandEmpty>
          </CommandRoot>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export interface CommandActionProps {
  /** What the user types to find this. Defaults to the visible text. */
  value?: string;

  onSelect?: () => void;

  disabled?: boolean;

  children: ReactNode;
}

export function CommandAction({
  value,
  onSelect,
  disabled = false,
  children,
}: CommandActionProps) {
  return (
    <CommandItem
      // Spread rather than pass, because `exactOptionalPropertyTypes` makes
      // "absent" and "present and undefined" different types, and cmdk's props
      // are declared as the former.
      {...(value === undefined ? {} : { value })}
      {...(onSelect === undefined ? {} : { onSelect })}
      disabled={disabled}
      className={cn(
        "flex cursor-default select-none items-center gap-2 rounded-token-sm",
        "px-2 py-2 text-sm outline-none",
        "data-[selected=true]:bg-accent-subtle data-[selected=true]:text-accent",
        "data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50",
      )}
    >
      {children}
    </CommandItem>
  );
}

/**
 * Opens the palette on a keystroke.
 *
 * Both Ctrl and Meta, because a product used on both platforms cannot ask
 * people to learn which one this build assumed. The listener is capture-phase
 * so a focused text input does not swallow it.
 */
export function useCommandPaletteHotkey(onOpen: () => void, key = "k"): void {
  useEffect(() => {
    function handle(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== key || !(event.metaKey || event.ctrlKey)) return;

      event.preventDefault();
      onOpen();
    }

    document.addEventListener("keydown", handle, true);

    return () => {
      document.removeEventListener("keydown", handle, true);
    };
  }, [onOpen, key]);
}

/**
 * Keeps `aria-activedescendant` on the input pointing at the active option.
 *
 * cmdk sets it, but only when the selection *changes*. So it is absent when
 * the palette opens — the first option is already highlighted and nothing
 * changed — and absent again whenever a search narrows to a single result,
 * because the selection stays where it was. Both are exactly the moments a
 * screen reader user needs to be told what is active, and in both of them the
 * highlight is visible to everyone else.
 *
 * Reproducible against cmdk on its own, so this is not something our wrapper
 * broke. axe does not catch it either: an absent attribute is not an invalid
 * one.
 *
 * The fix reads the DOM rather than cmdk's state, because the ids belong to
 * cmdk and deriving our own would mean two notions of "active" that have to be
 * kept in step. Writing only on a difference is what keeps the observer from
 * feeding itself.
 */
function useActiveDescendant(
  input: HTMLInputElement | null,
  list: HTMLDivElement | null,
): void {
  useEffect(() => {
    if (input === null || list === null) return;

    function sync() {
      if (input === null || list === null) return;

      const active = list.querySelector('[cmdk-item][aria-selected="true"]');
      const id = active === null ? null : active.id;

      if (id === null || id === "") {
        input.removeAttribute("aria-activedescendant");

        return;
      }

      if (input.getAttribute("aria-activedescendant") !== id) {
        input.setAttribute("aria-activedescendant", id);
      }
    }

    sync();

    const observer = new MutationObserver(sync);

    // The list, for a selection moving or the results changing; the input,
    // because a React re-render can drop the attribute we just set.
    observer.observe(list, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ["aria-selected"],
    });

    observer.observe(input, {
      attributes: true,
      attributeFilter: ["aria-activedescendant"],
    });

    return () => {
      observer.disconnect();
    };
  }, [input, list]);
}
