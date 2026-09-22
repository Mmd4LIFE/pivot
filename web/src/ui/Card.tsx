import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/cn";

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
}

/**
 * A bounded block of related content.
 *
 * Purely visual, with no role: a card is a box, and announcing "group" around
 * every box on a dashboard makes the page slower to navigate, not clearer.
 * When a card genuinely is a landmark — a dashboard panel with a heading —
 * pass `as`-style props through and give it a real section element instead.
 */
export function Card({ className, children, ...props }: CardProps) {
  return (
    <div
      className={cn(
        "rounded-token border border-line bg-surface shadow-token-sm",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
}

export function CardHeader({ className, children, ...props }: CardProps) {
  return (
    <div
      className={cn("flex flex-col gap-1 border-b border-line p-4", className)}
      {...props}
    >
      {children}
    </div>
  );
}

export interface CardTitleProps extends HTMLAttributes<HTMLHeadingElement> {
  /**
   * The heading level.
   *
   * Required, and deliberately not defaulted to h3. Heading level is a
   * property of where the card sits in the document, not of the card, and a
   * component that guesses produces a page whose outline skips levels —
   * which is how a screen reader user loses the ability to navigate by
   * heading.
   */
  level: 2 | 3 | 4 | 5 | 6;
  children: ReactNode;
}

export function CardTitle({ level, className, children, ...props }: CardTitleProps) {
  const Heading = `h${level}` as const;

  return (
    <Heading className={cn("text-base font-semibold text-content", className)} {...props}>
      {children}
    </Heading>
  );
}

export interface CardDescriptionProps extends HTMLAttributes<HTMLParagraphElement> {
  children: ReactNode;
}

export function CardDescription({ className, children, ...props }: CardDescriptionProps) {
  return (
    <p className={cn("text-sm text-content-muted", className)} {...props}>
      {children}
    </p>
  );
}

export function CardBody({ className, children, ...props }: CardProps) {
  return (
    <div className={cn("p-4", className)} {...props}>
      {children}
    </div>
  );
}

export function CardFooter({ className, children, ...props }: CardProps) {
  return (
    <div
      className={cn(
        "flex items-center justify-end gap-2 border-t border-line p-4",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
}
