import type { ComponentType, SVGProps } from "react";

import * as Glyph from "./glyphs";

/**
 * The navigation model, declared once.
 *
 * The sidebar, the breadcrumbs and the command palette all read this. Three
 * copies of the same list is how a renamed page ends up correct in two places
 * and wrong in the third, and the wrong one is usually the palette, because
 * nobody opens it during review.
 *
 * `titleKey` is a translation key rather than a string: the catalog is the
 * type, so a page added here without a string is a compile error.
 */

export interface NavItem {
  /** The route path. Must exist in the tree or the link is dead. */
  to: "/" | "/dashboards" | "/questions" | "/connections" | "/people" | "/settings";

  titleKey:
    | "nav.home"
    | "nav.dashboards"
    | "nav.questions"
    | "nav.connections"
    | "nav.people"
    | "nav.settings";

  icon: ComponentType<SVGProps<SVGSVGElement>>;

  /** Words that should find this page in the palette, beyond its title. */
  keywords?: string;
}

export const NAVIGATION: readonly NavItem[] = [
  { to: "/", titleKey: "nav.home", icon: Glyph.Home, keywords: "start overview" },
  {
    to: "/dashboards",
    titleKey: "nav.dashboards",
    icon: Glyph.Dashboard,
    keywords: "charts reports",
  },
  {
    to: "/questions",
    titleKey: "nav.questions",
    icon: Glyph.Question,
    keywords: "query sql explore",
  },
  {
    to: "/connections",
    titleKey: "nav.connections",
    icon: Glyph.Database,
    keywords: "database source warehouse postgres",
  },
  { to: "/people", titleKey: "nav.people", icon: Glyph.People, keywords: "members users roles" },
  {
    to: "/settings",
    titleKey: "nav.settings",
    icon: Glyph.Settings,
    keywords: "configuration admin organization",
  },
];

/** The item a path belongs to, for breadcrumbs and the active nav state. */
export function itemFor(pathname: string): NavItem | undefined {
  // Longest match first, so /settings/account belongs to /settings rather
  // than to /.
  return [...NAVIGATION]
    .sort((a, b) => b.to.length - a.to.length)
    .find((item) => item.to === "/" ? pathname === "/" : pathname.startsWith(item.to));
}
