import { Slot, Slottable } from "@radix-ui/react-slot";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/cn";
import { Spinner } from "./Spinner";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md" | "lg";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;

  /**
   * Render the child element instead of a <button>.
   *
   * For the case where a control has to be a link — "Continue with Okta" is
   * navigation, not a form submission, and a <button> there loses the middle
   * click, the context menu and the status bar preview a link gives for free.
   */
  asChild?: boolean;

  /** Show a spinner and disable interaction. */
  loading?: boolean;

  children?: ReactNode;
}

const VARIANTS: Record<ButtonVariant, string> = {
  primary: "bg-accent text-accent-content hover:bg-accent-hover",
  secondary:
    "bg-surface text-content border border-line-strong hover:bg-surface-sunken",
  ghost: "bg-transparent text-content hover:bg-surface-sunken",
  danger: "bg-danger text-accent-content hover:bg-danger-hover",
};

const SIZES: Record<ButtonSize, string> = {
  // Height floors rather than padding alone: a 44px target is the mobile
  // guideline, and 36px is the smallest that stays comfortably clickable.
  sm: "h-9 px-3 text-sm gap-1.5",
  md: "h-10 px-4 text-sm gap-2",
  lg: "h-11 px-6 text-base gap-2",
};

export function Button({
  variant = "primary",
  size = "md",
  asChild = false,
  loading = false,
  disabled,
  className,
  children,
  ...props
}: ButtonProps) {
  const Component = asChild ? Slot : "button";

  return (
    <Component
      // `disabled` alone would remove the control from the tab order while it
      // loads, moving focus somewhere the user did not ask for. aria-busy tells
      // assistive technology what is happening without that side effect.
      aria-busy={loading || undefined}
      disabled={disabled || loading}
      className={cn(
        "inline-flex items-center justify-center rounded-token font-medium",
        "transition-colors",
        // Logical properties throughout: `dir="rtl"` has to be a one-line
        // change in Part 11, not a retrofit.
        "disabled:pointer-events-none disabled:opacity-50",
        VARIANTS[variant],
        SIZES[size],
        className,
      )}
      {...props}
    >
      {loading && <Spinner className="size-4" label={null} />}

      {/*
        Slottable, not a bare {children}. Slot needs exactly one child to merge
        onto, and the spinner beside it makes two — so `asChild` threw on every
        render, loading or not, until a story made it render.

        Outside a Slot, Slottable returns its children unchanged, so the plain
        <button> case is unaffected.
      */}
      <Slottable>{children}</Slottable>
    </Component>
  );
}
