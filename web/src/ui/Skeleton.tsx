import { cn } from "../lib/cn";

export interface SkeletonProps {
  className?: string;
}

/**
 * A placeholder for content that is loading.
 *
 * Hidden from assistive technology entirely. A screen reader user gains
 * nothing from "blank, blank, blank" and would have to listen through it; the
 * component that owns the load announces the wait instead.
 */
export function Skeleton({ className }: SkeletonProps) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        "rounded-token bg-surface-sunken motion-safe:animate-pulse",
        className,
      )}
    />
  );
}
