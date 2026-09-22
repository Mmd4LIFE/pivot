import { Outlet, createRoute, redirect } from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import { Route as rootRoute } from "./root";
import { sessionQuery, useSession } from "../api/queries";
import { changeLocale } from "../i18n";
import { currentDestination } from "../lib/redirect";
import { OfflineBanner } from "../components/OfflineBanner";

/**
 * Everything behind the login.
 *
 * A pathless layout route, so `/dashboards` stays `/dashboards` rather than
 * becoming `/app/dashboards` — the guard is a property of the route tree, not
 * of the URL.
 *
 * The check runs in `beforeLoad`, which means it runs *before* the child route
 * loads any data. A guard in a component's render happens after the requests
 * it was supposed to prevent have already gone out, which is how a signed-out
 * user ends up generating a burst of 401s and a flash of empty interface.
 *
 * This is not the security boundary. The server refuses every request without
 * a valid session cookie, and it would do so with this file deleted. What this
 * does is keep an unauthenticated visitor from watching an empty application
 * shell fail to load, and it remembers where they were trying to go.
 *
 * Part 11-b puts the real shell here.
 */
export const Route = createRoute({
  getParentRoute: () => rootRoute,
  id: "authenticated",

  beforeLoad: async ({ context, location }) => {
    const session = await context.queryClient.ensureQueryData(sessionQuery);

    if (session === null) {
      throw redirect({
        to: "/login",
        search: { redirect: currentDestination(location) },
        replace: true,
      });
    }

    // Handed down to every child route, so a page never has to ask again or
    // handle a null it cannot reach.
    return { session };
  },

  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  useUserLocale();

  return (
    <>
      <OfflineBanner />
      <Outlet />
    </>
  );
}

/**
 * Follow the signed-in user's stored locale.
 *
 * Worth doing even though only English is translated, because direction is not
 * a translation. A user whose profile says `ar` gets a right-to-left layout
 * immediately, reading English until somebody writes the Arabic — which is the
 * right order, since the layout is the part that cannot be retrofitted.
 */
function useUserLocale(): void {
  const { i18n } = useTranslation();

  const session = useSession();
  const locale = session.data?.user.locale;

  useEffect(() => {
    if (locale === undefined || locale === "" || locale === i18n.language) return;

    void changeLocale(locale);
  }, [locale, i18n.language]);
}
