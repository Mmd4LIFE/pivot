import * as DialogPrimitive from "@radix-ui/react-dialog";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

/**
 * A modal dialog.
 *
 * The accessibility here is the whole component, and almost none of it is
 * visible: focus moves into the dialog on open and cannot leave it, Escape
 * closes, focus returns to whatever opened it, the page behind is hidden from
 * assistive technology and cannot scroll. Radix does all of that. What this
 * file adds is the styling and one rule Radix only warns about — a dialog must
 * have a title, so `title` is a required prop rather than a child somebody
 * might leave out.
 *
 * A title that should not be seen is still announced: pass `titleHidden`.
 */

export const DialogRoot = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export interface DialogProps {
  title: ReactNode;

  /** Hide the title visually. It stays in the accessible name. */
  titleHidden?: boolean;

  /**
   * A sentence describing the dialog's purpose.
   *
   * Announced with the title when present. Radix wires `aria-describedby`
   * itself and leaves it off when there is no description, so there is nothing
   * to do here beyond rendering it.
   */
  description?: ReactNode;

  /** The buttons. Rendered in a footer, end-aligned. */
  footer?: ReactNode;

  className?: string;
  children?: ReactNode;
}

export function DialogContent({
  title,
  titleHidden = false,
  description,
  footer,
  className,
  children,
}: DialogProps) {
  return (
    <DialogPrimitive.Portal>
      {/*
        The scrim is not a close button. Radix closes on a click outside
        already, and giving the scrim its own role would announce a control
        that duplicates Escape.
      */}
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-overlay/50" />

      <DialogPrimitive.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-50 w-[calc(100vw-2rem)] max-w-lg",
          "-translate-x-1/2 -translate-y-1/2",
          "rounded-token border border-line bg-surface-raised text-content",
          "shadow-token-lg",
          "max-h-[calc(100vh-4rem)] overflow-y-auto",
          className,
        )}
      >
        <div className="flex flex-col gap-1 p-4">
          <DialogPrimitive.Title
            className={cn(
              titleHidden ? "sr-only" : "text-base font-semibold text-content",
            )}
          >
            {title}
          </DialogPrimitive.Title>

          {description && (
            <DialogPrimitive.Description className="text-sm text-content-muted">
              {description}
            </DialogPrimitive.Description>
          )}
        </div>

        {children && <div className="px-4 pb-4 text-sm">{children}</div>}

        {footer && (
          <div className="flex items-center justify-end gap-2 border-t border-line p-4">
            {footer}
          </div>
        )}

        <DialogPrimitive.Close
          // An explicit close, because Escape is invisible and a pointer user
          // who does not know about the scrim has nothing else to aim at.
          className={cn(
            "absolute end-3 top-3 rounded-token-sm p-1",
            "text-content-muted hover:bg-surface-sunken hover:text-content",
          )}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true" className="size-4">
            <path
              d="M4 4l8 8M12 4l-8 8"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.75"
              strokeLinecap="round"
            />
          </svg>
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
