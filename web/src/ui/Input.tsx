import type { ComponentPropsWithRef } from "react";
import { cn } from "../lib/cn";
import { useFieldControlProps } from "./Field";

// ComponentPropsWithRef rather than InputHTMLAttributes, so a caller can hold
// a ref to the element -- which the login page needs to move focus onto a
// field the server has just asked for. React 19 passes `ref` as an ordinary
// prop, so there is no forwardRef wrapper to add.
export type InputProps = ComponentPropsWithRef<"input">;

/** A text input. Must be used inside a [Field], which supplies its label. */
export function Input({ className, ...props }: InputProps) {
  const fieldProps = useFieldControlProps();

  return (
    <input
      {...fieldProps}
      className={cn(
        "h-10 w-full rounded-token bg-surface px-3 text-sm text-content",
        // border-strong rather than border: this is a control's boundary, and
        // WCAG 1.4.11 wants 3:1 for that. See tokens.css.
        "border border-line-strong",
        "placeholder:text-content-subtle",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "aria-[invalid]:border-danger",
        className,
      )}
      {...props}
    />
  );
}
