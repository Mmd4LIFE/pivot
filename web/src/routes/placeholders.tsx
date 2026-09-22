import { createRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { PageHeader } from "../components/shell/AppShell";
import { EmptyState } from "../ui/EmptyState";

/**
 * The pages the shell navigates to, before the phases that own them arrive.
 *
 * They exist rather than being left out, and that is a deliberate call. A
 * sidebar whose links do nothing is worse than a sidebar with fewer links, and
 * a link to a route that does not exist lands on "page not found" — which
 * looks like a bug rather than like work not yet done.
 *
 * Each one says plainly what will be here and which phase builds it. An empty
 * state that explains itself is a product decision; a blank page is an
 * accident.
 */

interface Placeholder {
  path: "/dashboards" | "/questions" | "/connections" | "/people" | "/settings";
  titleKey: "pages.dashboards.title" | "pages.questions.title" | "pages.connections.title" | "pages.people.title" | "pages.settings.title";
  emptyTitleKey:
    | "pages.dashboards.emptyTitle"
    | "pages.questions.emptyTitle"
    | "pages.connections.emptyTitle"
    | "pages.people.emptyTitle"
    | "pages.settings.emptyTitle";
  emptyBodyKey:
    | "pages.dashboards.emptyBody"
    | "pages.questions.emptyBody"
    | "pages.connections.emptyBody"
    | "pages.people.emptyBody"
    | "pages.settings.emptyBody";
}

function placeholderRoute(spec: Placeholder) {
  return createRoute({
    getParentRoute: () => authenticatedRoute,
    path: spec.path,
    component: function Placeholder() {
      const { t } = useTranslation();

      return (
        <>
          <PageHeader title={t(spec.titleKey)} />

          <EmptyState title={t(spec.emptyTitleKey)} description={t(spec.emptyBodyKey)} />
        </>
      );
    },
  });
}

export const dashboardsRoute = placeholderRoute({
  path: "/dashboards",
  titleKey: "pages.dashboards.title",
  emptyTitleKey: "pages.dashboards.emptyTitle",
  emptyBodyKey: "pages.dashboards.emptyBody",
});

export const questionsRoute = placeholderRoute({
  path: "/questions",
  titleKey: "pages.questions.title",
  emptyTitleKey: "pages.questions.emptyTitle",
  emptyBodyKey: "pages.questions.emptyBody",
});

export const connectionsRoute = placeholderRoute({
  path: "/connections",
  titleKey: "pages.connections.title",
  emptyTitleKey: "pages.connections.emptyTitle",
  emptyBodyKey: "pages.connections.emptyBody",
});

export const peopleRoute = placeholderRoute({
  path: "/people",
  titleKey: "pages.people.title",
  emptyTitleKey: "pages.people.emptyTitle",
  emptyBodyKey: "pages.people.emptyBody",
});

export const settingsRoute = placeholderRoute({
  path: "/settings",
  titleKey: "pages.settings.title",
  emptyTitleKey: "pages.settings.emptyTitle",
  emptyBodyKey: "pages.settings.emptyBody",
});
