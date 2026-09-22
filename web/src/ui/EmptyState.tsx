import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export interface EmptyStateProps {
  title: ReactNode;

  /** Why it is empty and what to do about it. */
  description?: ReactNode;

  /** The action that fills it — usually a [Button]. */
  action?: ReactNode;

  className?: string;
}

/**
 * What a list shows when it has nothing in it.
 *
 * Distinct from a loading state and from an error, and all three have to look
 * different. "No dashboards yet" and "could not load dashboards" mean opposite
 * things to the person reading them, and a shared grey box for both is how
 * users come to believe the product lost their data.
 */
export function EmptyState({ title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-center gap-2 rounded-token border border-dashed",
        "border-line-strong bg-surface p-8 text-center",
        className,
      )}
    >
      <p className="text-sm font-medium text-content">{title}</p>

      {description && (
        <p className="max-w-prose text-sm text-content-muted">{description}</p>
      )}

      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}
