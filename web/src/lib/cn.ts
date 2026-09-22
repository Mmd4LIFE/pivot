import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Compose class names, with later Tailwind utilities winning.
 *
 * Plain string concatenation does not work for a component library: a caller
 * passing `className="p-0"` to a component whose base is `p-4` gets both, and
 * which one applies then depends on the order they happen to appear in the
 * compiled stylesheet rather than on the caller's intent. twMerge resolves
 * conflicts by Tailwind's own grouping, so the caller always wins.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
