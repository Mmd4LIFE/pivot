import { Link, createRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { useQueryableConnections } from "../api/queries";
import { PageHeader } from "../components/shell/AppShell";
import { Card, CardBody } from "../ui/Card";
import { EmptyState } from "../ui/EmptyState";
import { Skeleton } from "../ui/Skeleton";

/**
 * What is connected, before anybody writes a query.
 *
 * The first screen somebody wants and the last one this product built: an
 * editor is useless to a person who does not yet know what their warehouse
 * contains, and "open a tool and stare at an empty box" is where most first
 * sessions end.
 *
 * Read from the catalog, so it costs the source nothing -- Part 19-b built the
 * catalog for exactly this kind of question.
 */

export const Route = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/browse",
  component: BrowsePage,
});

function BrowsePage() {
  const { t } = useTranslation();
  const connections = useQueryableConnections();

  const available = connections.data?.connections ?? [];

  return (
    <>
      <PageHeader title={t("browse.title")} description={t("browse.subtitle")} />

      {connections.isPending ? <Skeleton className="h-32 w-full" /> : null}

      {!connections.isPending && available.length === 0 ? (
        <EmptyState
          title={t("editor.noConnections")}
          description={t("editor.noConnectionsBody")}
        />
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {available.map((connection) => (
          <Link
            key={connection.id}
            to="/browse/$connectionId"
            params={{ connectionId: connection.id }}
            className="rounded-token focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
          >
            <Card className="h-full transition-colors hover:border-line-strong">
              <CardBody>
                <div className="flex items-baseline justify-between gap-2">
                  <span className="truncate font-medium text-content">{connection.name}</span>

                  <span className="shrink-0 text-[10px] uppercase tracking-wide text-content-subtle">
                    {connection.kind}
                  </span>
                </div>

                <p className="mt-1 truncate text-sm text-content-muted">{connection.slug}</p>
              </CardBody>
            </Card>
          </Link>
        ))}
      </div>
    </>
  );
}
