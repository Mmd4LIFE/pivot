import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { OfflineBanner } from "../OfflineBanner";
import { Breadcrumbs } from "./Breadcrumbs";
import { CommandMenu } from "./CommandMenu";
import { SideNav } from "./SideNav";
import { UserMenu } from "./UserMenu";

/**
 * The application shell.
 *
 * Four landmarks and one of each: a banner, a navigation, a main, and inside
 * the header a second navigation for the breadcrumbs — which is why both are
 * labeled. A screen reader user navigates a page by its landmarks, and
 * "navigation, navigation, navigation" is the same as having none.
 *
 * `<main>` appears exactly once and carries `id="content"`, because the skip
 * link points at it. The skip link is the first thing in the tab order and is
 * invisible until focused: without one, a keyboard user tabs through the
 * entire sidebar on every single page before reaching what they came for.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const { t } = useTranslation();

  return (
    <div className="min-h-screen bg-canvas text-content">
      {/*
        Visually hidden until focused, then a real, visible button. `sr-only`
        alone would leave it invisible to the sighted keyboard user it exists
        for.
      */}
      <a
        href="#content"
        className={
          "sr-only focus:not-sr-only focus:absolute focus:start-4 focus:top-4 focus:z-50 " +
          "focus:rounded-token focus:bg-accent focus:px-4 focus:py-2 " +
          "focus:font-medium focus:text-accent-content"
        }
      >
        {t("nav.skipToContent")}
      </a>

      <div className="flex min-h-screen">
        {/*
          Hidden below `md` rather than collapsed into a drawer. A drawer is a
          real component with a focus trap and an escape route, and half of one
          is worse than none -- Part 14 builds it properly. The command palette
          is the interim answer on a small screen, which is why its button is
          always visible.
        */}
        <aside className="hidden w-56 shrink-0 border-e border-line bg-surface md:flex md:flex-col">
          <div className="flex h-14 items-center px-4">
            <Link to="/" className="text-base font-semibold">
              {t("app.name")}
            </Link>
          </div>

          <SideNav className="px-2" />
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          <header className="flex h-14 items-center justify-between gap-4 border-b border-line bg-surface px-4">
            <Breadcrumbs />

            <div className="flex items-center gap-2">
              <CommandMenu />
              <UserMenu />
            </div>
          </header>

          <OfflineBanner />

          <main id="content" tabIndex={-1} className="min-w-0 flex-1 p-6">
            {children}
          </main>
        </div>
      </div>
    </div>
  );
}

/**
 * A page's heading block.
 *
 * `<h1>` lives here and nowhere else, so every page has exactly one and the
 * outline cannot skip a level. A page that renders its own heading is a page
 * that eventually renders two.
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-6 flex items-start justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold">{title}</h1>

        {description && <p className="mt-1 text-sm text-content-muted">{description}</p>}
      </div>

      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
