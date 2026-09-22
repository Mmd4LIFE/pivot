import * as LabelPrimitive from "@radix-ui/react-label";
import { createContext, useContext, useId, type ReactNode } from "react";
import { cn } from "../lib/cn";

/**
 * A labeled form control with description and error text.
 *
 * It exists so that the wiring cannot be forgotten. A label needs `htmlFor`, a
 * description needs to be in `aria-describedby`, an error needs to be in there
 * too *and* to set `aria-invalid` — and every one of those is a separate thing
 * to remember on every input in the product. Field generates the ids once and
 * hands them to the control, so the accessible version is the easy one.
 */

interface FieldContextValue {
  controlId: string;
  descriptionId: string | undefined;
  errorId: string | undefined;
  invalid: boolean;
}

const FieldContext = createContext<FieldContextValue | null>(null);

/** The ids and state a control needs. Throws outside a Field, deliberately. */
export function useField(): FieldContextValue {
  const context = useContext(FieldContext);

  if (context === null) {
    throw new Error("useField must be used inside a <Field>");
  }

  return context;
}

/**
 * The props a control spreads onto itself to become accessible.
 *
 * Returned as an object rather than applied by cloning, because cloning breaks
 * the moment a caller wraps their control in anything.
 */
export function useFieldControlProps() {
  const { controlId, descriptionId, errorId, invalid } = useField();

  const describedBy = [descriptionId, errorId].filter(Boolean).join(" ");

  return {
    id: controlId,
    "aria-invalid": invalid || undefined,
    "aria-describedby": describedBy === "" ? undefined : describedBy,
  } as const;
}

export interface FieldProps {
  label: ReactNode;

  /** Helper text, announced with the control. */
  description?: ReactNode;

  /**
   * An error message.
   *
   * Presence is what marks the control invalid — there is no separate
   * `invalid` prop to fall out of step with it.
   */
  error?: ReactNode;

  /** Marks the control required, visually and to assistive technology. */
  required?: boolean;

  className?: string;
  children: ReactNode;
}

export function Field({
  label,
  description,
  error,
  required = false,
  className,
  children,
}: FieldProps) {
  const base = useId();

  const value: FieldContextValue = {
    controlId: `${base}-control`,
    descriptionId: description ? `${base}-description` : undefined,
    errorId: error ? `${base}-error` : undefined,
    invalid: Boolean(error),
  };

  return (
    <FieldContext.Provider value={value}>
      <div className={cn("flex flex-col gap-1.5", className)}>
        <LabelPrimitive.Root
          htmlFor={value.controlId}
          className="text-sm font-medium text-content"
        >
          {label}
          {required && (
            <>
              {/*
                The asterisk is decorative: "required" is already conveyed by
                the control's own required attribute, and announcing "star"
                mid-label helps nobody.
              */}
              <span aria-hidden="true" className="ms-1 text-danger">
                *
              </span>
              <span className="sr-only"> (required)</span>
            </>
          )}
        </LabelPrimitive.Root>

        {children}

        {description && (
          <p id={value.descriptionId} className="text-sm text-content-muted">
            {description}
          </p>
        )}

        {error && (
          <p
            id={value.errorId}
            // The error appears after the user has acted, so it has to be
            // announced rather than silently added to the page.
            role="alert"
            className="text-sm text-danger"
          >
            {error}
          </p>
        )}
      </div>
    </FieldContext.Provider>
  );
}
