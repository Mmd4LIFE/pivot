import type { SVGProps } from "react";

/**
 * The editor's toolbar icons.
 *
 * Hand-drawn, for the reason the shell's glyphs are: four paths weigh nothing,
 * and an icon package is a dependency, a licence and a slice of a 200 KB
 * budget for something that is genuinely four paths.
 *
 * Kept apart from `shell/glyphs` because these are not navigation. A toolbar
 * icon stands in for a verb and carries an `aria-label` on the button around
 * it; a navigation icon sits beside a visible name and must stay silent.
 *
 * Every one is `aria-hidden`: the button says what it does.
 */

function Icon({ children, ...props }: SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className="size-4 shrink-0"
      {...props}
    >
      {children}
    </svg>
  );
}

/** Run. A filled triangle, because this one is the primary action. */
export function Play(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M6 4.2v11.6a.6.6 0 0 0 .92.5l9-5.8a.6.6 0 0 0 0-1l-9-5.8a.6.6 0 0 0-.92.5z" fill="currentColor" />
    </Icon>
  );
}

/** Format. Ragged lines made even -- which is what the button does. */
export function Format(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M3 5h14M3 9h8M6 13h11M6 17h6" />
    </Icon>
  );
}

/** The catalog: a table of rows and columns. */
export function Table(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <rect x="3" y="4" width="14" height="12" rx="1.5" />
      <path d="M3 8h14M8.5 8v8" />
    </Icon>
  );
}

/** Copy link. Two halves of a chain. */
export function Link(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M8.5 11.5a3 3 0 0 0 4.24 0l2.3-2.3a3 3 0 0 0-4.24-4.24l-1.1 1.1" />
      <path d="M11.5 8.5a3 3 0 0 0-4.24 0l-2.3 2.3a3 3 0 1 0 4.24 4.24l1.1-1.1" />
    </Icon>
  );
}

/** Copied. The only feedback a clipboard write gives is the one we draw. */
export function Check(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M4 10.5 8 14.5 16 6" />
    </Icon>
  );
}

/** Download. An arrow into a tray — the verb for "get this off the server". */
export function Download(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M10 3v10" />
      <path d="M6.5 9.5 10 13l3.5-3.5" />
      <path d="M4 16.5h12" />
    </Icon>
  );
}
