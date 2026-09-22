import * as AvatarPrimitive from "@radix-ui/react-avatar";
import { cn } from "../lib/cn";

export interface AvatarProps {
  /** The person's name. Used for the fallback initials and the alt text. */
  name: string;
  src?: string | undefined;
  className?: string;
}

/**
 * A user's picture, falling back to initials.
 *
 * Radix's fallback waits for the image to fail rather than rendering both,
 * which avoids the flash of initials that appears under a slow avatar.
 */
export function Avatar({ name, src, className }: AvatarProps) {
  return (
    <AvatarPrimitive.Root
      className={cn(
        "inline-flex size-8 shrink-0 select-none items-center justify-center",
        "overflow-hidden rounded-full bg-surface-sunken align-middle",
        className,
      )}
    >
      {src && (
        <AvatarPrimitive.Image
          src={src}
          // The name, not "avatar": a screen reader announcing "image, avatar"
          // has told the user nothing they could not guess.
          alt={name}
          className="size-full object-cover"
        />
      )}

      <AvatarPrimitive.Fallback
        className="text-xs font-medium text-content-muted"
        // Without a delay, the initials flash before a cached image paints.
        delayMs={src ? 300 : 0}
      >
        <span aria-hidden="true">{initials(name)}</span>
        <span className="sr-only">{name}</span>
      </AvatarPrimitive.Fallback>
    </AvatarPrimitive.Root>
  );
}

/** Up to two initials, from the first and last word of a name. */
function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);

  if (words.length === 0) return "?";

  const first = words[0]?.[0] ?? "";
  const last = words.length > 1 ? (words[words.length - 1]?.[0] ?? "") : "";

  return (first + last).toUpperCase();
}
