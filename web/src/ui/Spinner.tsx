import { cn } from "../lib/cn";

export interface SpinnerProps {
  className?: string;

  /**
   * What a screen reader announces.
   *
   * `null` hides it entirely, which is right when the spinner sits inside a
   * control that already says what is happening — a loading button would
   * otherwise announce "Loading. Save", which is worse than either alone.
   */
  label?: string | null;
}

export function Spinner({ className, label = "Loading" }: SpinnerProps) {
  return (
    <span
      // A spinner is decorative when something else describes the wait, so the
      // role is conditional rather than always "status".
      role={label === null ? undefined : "status"}
      aria-hidden={label === null || undefined}
      className={cn("inline-block", className)}
    >
      <svg
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
        // motion-safe: a user who has asked for reduced motion gets a static
        // indicator rather than a spinning one, which the base stylesheet's
        // prefers-reduced-motion rule also enforces.
        className="size-full motion-safe:animate-spin"
      >
        <circle
          cx="12"
          cy="12"
          r="10"
          stroke="currentColor"
          strokeWidth="3"
          className="opacity-25"
        />
        <path
          d="M12 2a10 10 0 0 1 10 10"
          stroke="currentColor"
          strokeWidth="3"
          strokeLinecap="round"
        />
      </svg>
      {label !== null && <span className="sr-only">{label}</span>}
    </span>
  );
}
