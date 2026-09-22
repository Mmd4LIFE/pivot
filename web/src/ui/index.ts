/**
 * Pivot's design system.
 *
 * Every component here is built on the semantic tokens in styles/tokens.css,
 * never on a hard-coded color, which is what makes Phase 8's white-label
 * embedding a configuration change rather than a rebuild.
 *
 * The scope is deliberate: these are the components Phases 1 and 2 need, and
 * nothing else. Phase 0 names a general-purpose component library as a risk to
 * this project, so a component is added here when a screen needs it.
 */

export { Alert } from "./Alert";
export type { AlertProps, AlertTone } from "./Alert";

export { Avatar } from "./Avatar";
export type { AvatarProps } from "./Avatar";

export { Badge } from "./Badge";
export type { BadgeProps, BadgeTone } from "./Badge";

export { Button } from "./Button";
export type { ButtonProps, ButtonSize, ButtonVariant } from "./Button";

export {
  Card,
  CardBody,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "./Card";
export type { CardDescriptionProps, CardProps, CardTitleProps } from "./Card";

export { Checkbox } from "./Checkbox";
export type { CheckboxProps } from "./Checkbox";

export { EmptyState } from "./EmptyState";
export type { EmptyStateProps } from "./EmptyState";

export { Field, useField, useFieldControlProps } from "./Field";
export type { FieldProps } from "./Field";

export { Input } from "./Input";
export type { InputProps } from "./Input";

export { Radio, RadioGroup } from "./RadioGroup";
export type { RadioGroupProps, RadioProps } from "./RadioGroup";

export { Separator } from "./Separator";
export type { SeparatorProps } from "./Separator";

export { Skeleton } from "./Skeleton";
export type { SkeletonProps } from "./Skeleton";

export { Spinner } from "./Spinner";
export type { SpinnerProps } from "./Spinner";

export { Switch } from "./Switch";
export type { SwitchProps } from "./Switch";

export { TBody, THead, Table, Td, Th, Tr } from "./Table";
export type { TableProps, ThProps } from "./Table";

export { Textarea } from "./Textarea";
export type { TextareaProps } from "./Textarea";
