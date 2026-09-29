import { createRoute } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { ApiError, type QueryResult } from "../api/client";
import { useQueryableConnections, useRunQuery } from "../api/queries";
import { PageHeader } from "../components/shell/AppShell";
import { Alert } from "../ui/Alert";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Card, CardBody } from "../ui/Card";
import { EmptyState } from "../ui/EmptyState";
import { Skeleton } from "../ui/Skeleton";
import { TBody, THead, Table, Td, Th, Tr } from "../ui/Table";

/**
 * The SQL editor, in the plainest form that is honestly useful.
 *
 * A textarea rather than CodeMirror, deliberately and for this part only.
 * CodeMirror is the largest frontend dependency this product will take and it
 * does not fit the 14 KB left in the bundle budget, so it arrives in 23-b with
 * the code-splitting that decision requires. Shipping the endpoint behind a
 * textarea first means the load-bearing half -- the pipeline finally having a
 * production caller -- is provable in a browser rather than only in tests.
 *
 * What is here is what the Done-when asks for: pick a source, run a statement,
 * read the rows, and be told plainly when the source says no.
 */

export const Route = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/editor",
  component: EditorPage,
});

function EditorPage() {
  const { t } = useTranslation();

  return (
    <>
      <PageHeader title={t("editor.title")} description={t("editor.subtitle")} />
      <Editor />
    </>
  );
}

function Editor() {
  const { t } = useTranslation();
  const connections = useQueryableConnections();
  const run = useRunQuery();

  const [connectionId, setConnectionId] = useState("");
  const [sql, setSql] = useState("SELECT 1");

  const available = connections.data?.connections ?? [];

  // The first source, chosen once, so the common case of a single connection
  // needs no interaction at all.
  const selected = connectionId || available[0]?.id || "";

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!selected || !sql.trim()) return;
    run.mutate({ connectionId: selected, sql });
  }

  if (connections.isPending) return <Skeleton className="h-40 w-full" />;

  if (available.length === 0) {
    return (
      <EmptyState
        title={t("editor.noConnections")}
        description={t("editor.noConnectionsBody")}
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardBody>
          <form className="flex flex-col gap-3" onSubmit={onSubmit}>
            <div className="flex flex-wrap items-center gap-3">
              <label className="text-sm text-[--color-fg-muted]" htmlFor="editor-connection">
                {t("editor.source")}
              </label>

              <select
                id="editor-connection"
                className="rounded-md border border-[--color-border] bg-[--color-bg] px-2 py-1 text-sm"
                value={selected}
                onChange={(event) => setConnectionId(event.target.value)}
              >
                {available.map((connection) => (
                  <option key={connection.id} value={connection.id}>
                    {connection.name} ({connection.kind})
                  </option>
                ))}
              </select>
            </div>

            <textarea
              aria-label={t("editor.statement")}
              className="min-h-40 w-full rounded-md border border-[--color-border] bg-[--color-bg] p-3 font-mono text-sm"
              spellCheck={false}
              value={sql}
              onChange={(event) => setSql(event.target.value)}
            />

            <div className="flex items-center gap-3">
              <Button type="submit" disabled={run.isPending || !sql.trim()}>
                {run.isPending ? t("editor.running") : t("editor.run")}
              </Button>

              {run.data ? <ResultSummary result={run.data} /> : null}
            </div>
          </form>
        </CardBody>
      </Card>

      {run.error ? <QueryFailure error={run.error} /> : null}
      {run.data ? <Results result={run.data} /> : null}
    </div>
  );
}

/**
 * What the source said, shown as what it is.
 *
 * The server sends the source's own message for a rejected statement -- "no
 * such column: nope" is the whole answer -- so this shows it rather than
 * paraphrasing. 23-b puts it at the line it came from; here it is at least the
 * real text and not a generic failure.
 */
function QueryFailure({ error }: { error: unknown }) {
  const { t } = useTranslation();

  const message = error instanceof ApiError ? error.message : String(error);

  return (
    <Alert tone="danger" title={t("editor.failed")}>
      <p className="font-mono text-sm">{message}</p>
    </Alert>
  );
}

function ResultSummary({ result }: { result: QueryResult }) {
  const { t } = useTranslation();

  return (
    <div className="flex items-center gap-2 text-sm text-[--color-fg-muted]">
      <span>{t("editor.rows", { count: result.rowCount })}</span>
      <span>·</span>
      <span>{t("editor.tookMs", { ms: result.durationMs })}</span>

      {/* A cache hit is worth saying: it is why the second run was instant. */}
      {result.cacheStatus === "hit" ? <Badge tone="success">{t("editor.cached")}</Badge> : null}

      {/*
        Truncation is not a detail. The result met the row cap, and a grid that
        showed it silently would be presenting a partial answer as a whole one.
      */}
      {result.truncated ? <Badge tone="warning">{t("editor.truncated")}</Badge> : null}
    </div>
  );
}

function Results({ result }: { result: QueryResult }) {
  const { t } = useTranslation();

  if (result.rows.length === 0) {
    return <EmptyState title={t("editor.noRows")} description={t("editor.noRowsBody")} />;
  }

  return (
    <Card>
      <div className="overflow-auto">
        <Table caption={t("editor.resultsCaption", { count: result.rowCount })}>
          <THead>
            <Tr>
              {result.columns.map((column) => (
                <Th key={column.name}>
                  <span>{column.name}</span>
                  <span className="ml-2 font-normal text-[--color-fg-subtle]">
                    {column.sourceType}
                  </span>
                </Th>
              ))}
            </Tr>
          </THead>

          <TBody>
            {result.rows.map((row, rowIndex) => (
              <Tr key={rowIndex}>
                {row.map((cell, cellIndex) => (
                  <Td key={cellIndex}>{renderCell(cell)}</Td>
                ))}
              </Tr>
            ))}
          </TBody>
        </Table>
      </div>
    </Card>
  );
}

/**
 * A cell, rendered so that nothing is mistaken for something else.
 *
 * NULL is shown as a word in a dimmer colour rather than as an empty cell,
 * because an empty cell is also what an empty string looks like -- and the
 * connectors went to real trouble (Part 17's conformance suite has a property
 * for it) to keep those two apart all the way here.
 */
function renderCell(cell: unknown) {
  if (cell === null || cell === undefined) {
    return <span className="text-[--color-fg-subtle] italic">NULL</span>;
  }

  if (typeof cell === "boolean") return cell ? "true" : "false";
  if (typeof cell === "object") return JSON.stringify(cell);

  return String(cell);
}
