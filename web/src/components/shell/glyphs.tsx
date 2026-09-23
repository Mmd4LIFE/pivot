import type { SVGProps } from "react";

/**
 * The navigation icons.
 *
 * Hand-drawn rather than a library, for the same reason the glyphs in `src/ui`
 * are: six paths weigh nothing, and an icon package is a dependency, a bundle
 * and a licence for something that is genuinely six paths.
 *
 * Every one is `aria-hidden`. The label beside it is the name — an icon that
 * announces itself makes a screen reader read every nav item twice.
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

export function Home(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M3 8.5 10 3l7 5.5V16a1 1 0 0 1-1 1h-3v-5H7v5H4a1 1 0 0 1-1-1z" />
    </Icon>
  );
}

export function Dashboard(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <rect x="3" y="3" width="6" height="8" rx="1" />
      <rect x="11" y="3" width="6" height="5" rx="1" />
      <rect x="3" y="13" width="6" height="4" rx="1" />
      <rect x="11" y="10" width="6" height="7" rx="1" />
    </Icon>
  );
}

export function Question(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M3 16V9M7.7 16V4M12.3 16v-5M17 16V7" />
    </Icon>
  );
}

export function Database(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <ellipse cx="10" cy="5" rx="6.5" ry="2.5" />
      <path d="M3.5 5v10c0 1.4 2.9 2.5 6.5 2.5s6.5-1.1 6.5-2.5V5" />
      <path d="M3.5 10c0 1.4 2.9 2.5 6.5 2.5s6.5-1.1 6.5-2.5" />
    </Icon>
  );
}

export function People(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <circle cx="8" cy="7" r="2.75" />
      <path d="M2.5 17a5.5 5.5 0 0 1 11 0" />
      <path d="M14 5.4a2.6 2.6 0 0 1 0 5.2M15.5 12.4A4.6 4.6 0 0 1 18 16.5" />
    </Icon>
  );
}

// One person, for the account menu's own entry. People (above) is two, because
// that page is about everybody else.
export function Person(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <circle cx="10" cy="6.5" r="3.25" />
      <path d="M3.5 17.5a6.5 6.5 0 0 1 13 0" />
    </Icon>
  );
}

export function Settings(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <circle cx="10" cy="10" r="2.6" />
      <path d="M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M15.3 4.7l-1.4 1.4M6.1 13.9l-1.4 1.4" />
    </Icon>
  );
}

export function Search(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <circle cx="8.5" cy="8.5" r="5.5" />
      <path d="m12.8 12.8 4.2 4.2" />
    </Icon>
  );
}

export function SignOut(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M12 6V4a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v12a1 1 0 0 0 1 1h7a1 1 0 0 0 1-1v-2" />
      <path d="M8 10h9m0 0-2.5-2.5M17 10l-2.5 2.5" />
    </Icon>
  );
}
