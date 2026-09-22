import { Link, useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { cn } from "../../lib/cn";
import { NAVIGATION, itemFor } from "./navigation";

/**
 * The primary navigation.
 *
 * A real `<nav>` with a label, because a page with several navigation regions
 * gives a screen reader user a list of landmarks and "navigation, navigation"
 * tells them nothing about which is which.
 *
 * The current page is marked with `aria-current="page"` as well as a color.
 * Color alone leaves a color-blind user with no idea where they are, and
 * `aria-current` is what a screen reader announces.
 */
export function SideNav({ className }: { className?: string }) {
  const { t } = useTranslation();
  const location = useLocation();

  const active = itemFor(location.pathname);

  return (
    <nav aria-label={t("nav.primary")} className={cn("flex flex-col gap-0.5", className)}>
      {NAVIGATION.map((item) => {
        const current = item === active;

        return (
          <Link
            key={item.to}
            to={item.to}
            aria-current={current ? "page" : undefined}
            className={cn(
              "flex items-center gap-2.5 rounded-token px-2.5 py-2 text-sm",
              "transition-colors",
              current
                ? "bg-accent-subtle font-medium text-accent"
                : "text-content-muted hover:bg-surface-sunken hover:text-content",
            )}
          >
            <item.icon />
            {t(item.titleKey)}
          </Link>
        );
      })}
    </nav>
  );
}
