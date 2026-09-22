import * as ToastPrimitive from "@radix-ui/react-toast";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "../lib/cn";

/**
 * A transient message that does not interrupt.
 *
 * The rule a toast exists to obey: it announces without stealing focus. Radix
 * gives every toast `role="status"` — never `role="alert"` — and varies only
 * how urgently it is spoken. An error is announced at once (`assertive`);
 * everything else waits for a pause (`polite`), because interrupting someone
 * mid-sentence to say "saved" is worse than saying nothing.
 *
 * What follows from that: **a toast is never the only place an error appears.**
 * It is gone in five seconds, and a user who was looking elsewhere has no way
 * back to it. Anything that must be read again belongs in an [Alert] on the
 * thing it concerns.
 */

export type ToastTone = "neutral" | "success" | "warning" | "danger";

export const ToastProvider = ToastPrimitive.Provider;

/**
 * Where toasts appear.
 *
 * Mounted once, near the end of the document. F8 moves focus into it — that is
 * Radix's hotkey, and it is the reason a keyboard user is not stranded when a
 * toast carries an action.
 */
export function ToastViewport({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof ToastPrimitive.Viewport>) {
  return (
    <ToastPrimitive.Viewport
      className={cn(
        "fixed bottom-0 end-0 z-50 flex max-h-screen w-full flex-col gap-2 p-4",
        "sm:max-w-96",
        className,
      )}
      {...props}
    />
  );
}

const TONES: Record<ToastTone, string> = {
  neutral: "border-line",
  success: "border-success/50",
  warning: "border-warning/50",
  danger: "border-danger/50",
};

export interface ToastProps
  extends Omit<ComponentPropsWithoutRef<typeof ToastPrimitive.Root>, "title" | "type"> {
  title: ReactNode;
  description?: ReactNode;

  /** A single action. More than one belongs in a dialog, not a toast. */
  action?: ReactNode;

  tone?: ToastTone;
}

export function Toast({
  title,
  description,
  action,
  tone = "neutral",
  className,
  ...props
}: ToastProps) {
  return (
    <ToastPrimitive.Root
      // "foreground" is assertive and "background" is polite. An error is the
      // only thing worth interrupting for.
      type={tone === "danger" ? "foreground" : "background"}
      className={cn(
        "flex items-start gap-3 rounded-token border p-3",
        "bg-surface-raised text-content shadow-token-lg",
        TONES[tone],
        className,
      )}
      {...props}
    >
      <div className="min-w-0 flex-1">
        <ToastPrimitive.Title className="text-sm font-medium">{title}</ToastPrimitive.Title>

        {description && (
          <ToastPrimitive.Description className="mt-0.5 text-sm text-content-muted">
            {description}
          </ToastPrimitive.Description>
        )}
      </div>

      {action && (
        // altText is what a screen reader offers instead of the button, for
        // users who cannot reach a toast before it disappears. Radix requires
        // it, and the requirement is the right one.
        <ToastPrimitive.Action asChild altText={typeof title === "string" ? title : "Action"}>
          {action}
        </ToastPrimitive.Action>
      )}

      <ToastPrimitive.Close
        className={cn(
          "rounded-token-sm p-1 text-content-muted",
          "hover:bg-surface-sunken hover:text-content",
        )}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true" className="size-3.5">
          <path
            d="M4 4l8 8M12 4l-8 8"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
          />
        </svg>
        <span className="sr-only">Dismiss</span>
      </ToastPrimitive.Close>
    </ToastPrimitive.Root>
  );
}
