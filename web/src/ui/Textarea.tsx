import type { TextareaHTMLAttributes } from "react";
import { cn } from "../lib/cn";
import { useFieldControlProps } from "./Field";

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement>;

/** A multi-line text input. Must be used inside a [Field]. */
export function Textarea({ className, ...props }: TextareaProps) {
  const fieldProps = useFieldControlProps();

  return (
    <textarea
      {...fieldProps}
      className={cn(
        "min-h-20 w-full rounded-token bg-surface px-3 py-2 text-sm text-content",
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
