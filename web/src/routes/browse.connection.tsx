import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { openInWorkspace } from "./editor.workspace";
import type { SchemaTable } from "../api/client";
import { useConnectionSchema, useQueryableConnections } from "../api/queries";
import { PageHeader } from "../components/shell/AppShell";
import { Button } from "../ui/Button";
import { Card, CardBody } from "../ui/Card";
import { EmptyState } from "../ui/EmptyState";
import { Field } from "../ui/Field";
import { Input } from "../ui/Input";
import { Skeleton } from "../ui/Skeleton";

/**
 * What is in one connected source.
 *
 * The tables the last sync saw, grouped by schema, with their columns. Nothing
 * here touches the source: this is the catalog, which is why it is instant and
 * why it can be honest about being a snapshot.
 */

export const Route = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/browse/$connectionId",
  component: BrowseConnectionPage,
});

function BrowseConnectionPage() {
  const { t } = useTranslation();
  const { connectionId } = Route.useParams();
  const navigate = useNavigate();

  const connections = useQueryableConnections();
  const schema = useConnectionSchema(connectionId);

  const [filter, setFilter] = useState("");

  const connection = connections.data?.connections.find((c) => c.id === connectionId);

  const grouped = useMemo(() => {
    const tables = schema.data?.tables ?? [];
    const needle = filter.trim().toLowerCase();

    const matching = needle
      ? tables.filter(
          (table) =>
            table.name.toLowerCase().includes(needle) ||
            table.columns.some((column) => column.toLowerCase().includes(needle)),
        )
      : tables;

    const bySchema = new Map<string, SchemaTable[]>();

    for (const table of matching) {
      const key = table.schema || "";
      bySchema.set(key, [...(bySchema.get(key) ?? []), table]);
    }

    return [...bySchema.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [schema.data, filter]);

  function open(table: SchemaTable) {
    openInWorkspace(table.name, previewStatement(table, connection?.kind ?? ""), connectionId);

    void navigate({ to: "/editor" });
  }

  return (
    <>
      <PageHeader
        title={connection?.name ?? t("browse.title")}
        description={t("browse.connectionSubtitle")}
      />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Link to="/browse" className="text-sm text-content-muted underline-offset-2 hover:underline">
          ← {t("browse.allSources")}
        </Link>

        {/*
          Wrapped in a Field, because Input requires one: it reads the id, the
          description and the invalid state from that context and throws
          without it. TypeScript cannot see a runtime context requirement, so
          this page rendered a blank screen and an error boundary -- which is
          what the route test now catches.
        */}
        <Field label={t("browse.filter")} className="max-w-xs">
          <Input
            placeholder={t("browse.filter")}
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </Field>
      </div>

      {schema.isPending ? <Skeleton className="h-32 w-full" /> : null}

      {/*
        An unsynced connection and an empty database produce the same empty
        list, and only one of them is worth telling somebody about. Saying "no
        tables" about a catalog nobody has read would have people looking for a
        database that is fine.
      */}
      {schema.data && !schema.data.synced ? (
        <EmptyState
          title={t("browse.notSynced")}
          description={t("browse.notSyncedBody", { slug: connection?.slug ?? "" })}
        />
      ) : null}

      {schema.data?.synced && grouped.length === 0 ? (
        <EmptyState title={t("browse.noMatches")} description={t("browse.noMatchesBody")} />
      ) : null}

      <div className="flex flex-col gap-6">
        {grouped.map(([schemaName, tables]) => (
          <section key={schemaName}>
            {schemaName ? (
              <h2 className="mb-2 text-xs uppercase tracking-wide text-content-subtle">
                {schemaName}
              </h2>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {tables.map((table) => (
                <Card key={schemaName + "." + table.name} className="h-full">
                  <CardBody>
                    <div className="flex items-start justify-between gap-2">
                      <span className="truncate font-medium text-content">{table.name}</span>

                      <Button size="sm" variant="ghost" onClick={() => open(table)}>
                        {t("browse.open")}
                      </Button>
                    </div>

                    <p className="mt-1 text-xs text-content-muted">
                      {t("browse.columns", { count: table.columns.length })}
                    </p>

                    <p className="mt-1 truncate text-xs text-content-subtle" title={table.columns.join(", ")}>
                      {table.columns.slice(0, 6).join(", ")}
                      {table.columns.length > 6 ? " …" : ""}
                    </p>
                  </CardBody>
                </Card>
              ))}
            </div>
          </section>
        ))}
      </div>
    </>
  );
}

/**
 * The statement that opens a table.
 *
 * Quoted for the source's own dialect, because an unquoted identifier breaks
 * on the first table somebody named `order` or `Select` -- and those exist.
 * MySQL uses backticks; everything else Pivot speaks to uses double quotes.
 *
 * A LIMIT, because "show me this table" means the first screen of it. Without
 * one, clicking a hundred-million-row table is a mistake that takes a warehouse
 * with it.
 */
export function previewStatement(table: SchemaTable, kind: string): string {
  const quote = kind === "mysql" ? "`" : '"';

  const name = (part: string) => `${quote}${part.replaceAll(quote, quote + quote)}${quote}`;

  const qualified = table.schema ? `${name(table.schema)}.${name(table.name)}` : name(table.name);

  return `SELECT *\nFROM ${qualified}\nLIMIT 100`;
}
