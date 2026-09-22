import { Link, useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { itemFor } from "./navigation";

/**
 * Where you are.
 *
 * An ordered list inside a labeled `<nav>`, which is the pattern assistive
 * technology expects: the order carries the hierarchy, and the label
 * distinguishes this navigation region from the sidebar.
 *
 * The last crumb is the current page and is not a link. Linking to the page
 * you are already on is a control that does nothing, and a keyboard user has
 * to tab through it to reach one that does.
 */
export function Breadcrumbs() {
  const { t } = useTranslation();
  const location = useLocation();

  const item = itemFor(location.pathname);

  if (item === undefined) return null;

  const atRoot = item.to === "/";

  return (
    <nav aria-label={t("nav.breadcrumbs")}>
      <ol className="flex items-center gap-1.5 text-sm">
        {!atRoot && (
          <>
            <li>
              <Link to="/" className="text-content-muted hover:text-content">
                {t("nav.home")}
              </Link>
            </li>

            <li aria-hidden="true" className="text-content-subtle">
              /
            </li>
          </>
        )}

        <li>
          {/*
            aria-current, not a disabled link: the crumb is still text a screen
            reader reads, it is simply the page you are on.
          */}
          <span aria-current="page" className="font-medium text-content">
            {t(item.titleKey)}
          </span>
        </li>
      </ol>
    </nav>
  );
}
