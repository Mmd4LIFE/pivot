import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type AlertTone = "info" | "success" | "warning" | "danger";

export interface AlertProps {
  tone?: AlertTone;
  title?: ReactNode;
  children: ReactNode;

  /**
   * Announce this alert when it appears.
   *
   * Off by default, and that default is the important part. A live region
   * interrupts whatever a screen reader is reading; one that is on the page
   * from the first paint interrupts the page's own heading. Turn it on for a
   * message that appears *in response to something the user did* — a failed
   * save — and leave it off for a banner that was always there.
   */
  live?: boolean;

  className?: string;
}

const TONES: Record<AlertTone, { box: string; icon: string; prefix: string }> = {
  info: {
    box: "border-info/40 bg-surface text-content",
    icon: "text-info",
    prefix: "Information:",
  },
  success: {
    box: "border-success/40 bg-surface text-content",
    icon: "text-success",
    prefix: "Success:",
  },
  warning: {
    box: "border-warning/40 bg-surface text-content",
    icon: "text-warning",
    prefix: "Warning:",
  },
  danger: {
    box: "border-danger/40 bg-surface text-content",
    icon: "text-danger",
    prefix: "Error:",
  },
};

/**
 * A message about the state of the page or of something the user just did.
 *
 * Tone is carried three ways: a color, a glyph, and a visually hidden word.
 * That redundancy is the point — a color-blind user gets the glyph, a screen
 * reader user gets the word, and neither has to infer "this one is the error"
 * from a border that is 40% red.
 */
export function Alert({
  tone = "info",
  title,
  children,
  live = false,
  className,
}: AlertProps) {
  const style = TONES[tone];

  return (
    <div
      // "alert" is assertive and interrupts; "status" is polite and waits for a
      // pause. Errors earn the interruption, everything else does not.
      role={live ? (tone === "danger" ? "alert" : "status") : undefined}
      className={cn(
        "flex gap-3 rounded-token border p-3 text-sm",
        style.box,
        className,
      )}
    >
      <AlertIcon tone={tone} className={cn("mt-0.5 size-4 shrink-0", style.icon)} />

      <div className="min-w-0 flex-1">
        <span className="sr-only">{style.prefix} </span>

        {title && <p className="font-medium text-content">{title}</p>}

        <div className={cn("text-content-muted", title && "mt-0.5")}>{children}</div>
      </div>
    </div>
  );
}

/** The glyph for a tone. Decorative: the hidden prefix carries the meaning. */
function AlertIcon({ tone, className }: { tone: AlertTone; className?: string }) {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true" className={className} fill="currentColor">
      {tone === "success" ? (
        <path d="M8 0a8 8 0 1 0 0 16A8 8 0 0 0 8 0zm3.7 6.2-4.3 4.3a.9.9 0 0 1-1.3 0L4.3 8.7a.9.9 0 1 1 1.3-1.3l1.1 1.2 3.7-3.7a.9.9 0 1 1 1.3 1.3z" />
      ) : tone === "info" ? (
        <path d="M8 0a8 8 0 1 0 0 16A8 8 0 0 0 8 0zm0 3.2a1 1 0 1 1 0 2 1 1 0 0 1 0-2zm1 9.1H7V6.8h2z" />
      ) : (
        // Warning and error share the triangle: both say "stop and read this",
        // and the tone color plus the hidden prefix separate them.
        <path d="M7.1 1.5a1 1 0 0 1 1.8 0l6 11.2a1 1 0 0 1-.9 1.5H2a1 1 0 0 1-.9-1.5zM7 5.5v4h2v-4zm0 5.3v1.8h2v-1.8z" />
      )}
    </svg>
  );
}
