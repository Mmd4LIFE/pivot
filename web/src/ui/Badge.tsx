import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/cn";

export type BadgeTone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: BadgeTone;
  children: ReactNode;
}

const TONES: Record<BadgeTone, string> = {
  neutral: "bg-surface-sunken text-content-muted border-line",
  accent: "bg-accent-subtle text-accent border-accent/30",
  success: "bg-transparent text-success border-success/40",
  warning: "bg-transparent text-warning border-warning/40",
  danger: "bg-transparent text-danger border-danger/40",
  info: "bg-transparent text-info border-info/40",
};

/**
 * A short status label.
 *
 * Tone is color only, and color is never the sole carrier of meaning — the
 * text says what the badge means, which is what makes it work for a
 * color-blind reader. A badge reading only a colored dot would not.
 */
export function Badge({ tone = "neutral", className, children, ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-token-sm border px-2 py-0.5",
        "text-xs font-medium",
        TONES[tone],
        className,
      )}
      {...props}
    >
      {children}
    </span>
  );
}
