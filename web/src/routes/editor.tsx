import { createRoute } from "@tanstack/react-router";
import { Suspense, lazy, useEffect, useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { MAX_LINK_LENGTH, decodeQuestion, questionLink } from "./editor.link";
import { useWorkspace, type EditorTab } from "./editor.workspace";
import { ApiError, type ConnectionSchema, type QueryResult } from "../api/client";
import { useConnectionSchema, useQueryableConnections, useRunQuery } from "../api/queries";
import { PageHeader } from "../components/shell/AppShell";
import { Alert } from "../ui/Alert";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Card, CardBody } from "../ui/Card";
import { EmptyState } from "../ui/EmptyState";
import { Skeleton } from "../ui/Skeleton";

/*
 * CodeMirror, loaded when somebody opens this page and not before.
 *
 * A dynamic import so that Rollup emits it as its own chunk, which index.html
 * does not reference -- so a cold visit to the login page pays none of it, and
 * the 200 KB initial budget stays intact. Changing this to a static import
 * would move roughly 50 KB gzipped into every visit, and the bundle gate is
 * what would notice.
 */
const SqlEditor = lazy(() => import("../components/editor/SqlEditor"));

/*
 * The grid, on the same terms as the editor: loaded with the page rather than
 * with the application, so the virtualizer it brings costs nothing to somebody
 * who never opens this screen.
 */
const ResultGrid = lazy(() => import("../components/grid/ResultGrid"));

/**
 * The SQL editor.
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

  const { workspace, active, update, open, close, select, openWith } = useWorkspace();

  /*
   * A question that arrived in the URL.
   *
   * Opened as a new tab rather than replacing what somebody was writing: a
   * link is somebody else's question, and landing on it must not cost you
   * yours. The fragment is cleared afterwards so a reload does not open it a
   * second time, and so the address bar stops showing a link to something the
   * page has moved on from.
   */
  useEffect(() => {
    const shared = decodeQuestion(window.location.hash);
    if (!shared) return;

    openWith({
      title: shared.title || "Shared query",
      sql: shared.sql,
      connectionId: shared.connectionId,
    });

    window.history.replaceState(null, "", window.location.pathname);
  }, [openWith]);

  const available = connections.data?.connections ?? [];

  // The tab's own source, falling back to the first one so that the common
  // case -- one connection, a fresh tab -- needs no interaction at all.
  const selected = active.connectionId || available[0]?.id || "";
  const kind = available.find((connection) => connection.id === selected)?.kind ?? "";

  const schema = useConnectionSchema(selected);
  /*
   * Memoized, and that is not a micro-optimization.
   *
   * This object is a dependency of the editor's language configuration.
   * Building a fresh one on every render made the editor reconfigure on every
   * keystroke -- which, before the editor used a compartment, rebuilt it
   * outright and took the cursor with it.
   */
  const completion = useMemo(() => completionSchema(schema.data), [schema.data]);

  // Named, because two things start a query: the button and Ctrl-Enter. A
  // shortcut that does something subtly different from the button is worse
  // than no shortcut.
  function submit() {
    if (!selected || !active.sql.trim()) return;
    run.mutate({ connectionId: selected, sql: active.sql });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    submit();
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
      <Tabs
        tabs={workspace.tabs}
        activeId={workspace.activeId}
        onSelect={select}
        onOpen={open}
        onClose={close}
      />

      <Card>
        <CardBody>
          <form className="flex flex-col gap-3" onSubmit={onSubmit}>
            <div className="flex flex-wrap items-center gap-3">
              <label className="text-sm text-content-muted" htmlFor="editor-connection">
                {t("editor.source")}
              </label>

              <select
                id="editor-connection"
                className="rounded-md border border-line bg-surface px-2 py-1 text-sm"
                value={selected}
                onChange={(event) => update({ connectionId: event.target.value })}
              >
                {available.map((connection) => (
                  <option key={connection.id} value={connection.id}>
                    {connection.name} ({connection.kind})
                  </option>
                ))}
              </select>
            </div>

            <Suspense fallback={<Skeleton className="h-40 w-full" />}>
              <SqlEditor
                value={active.sql}
                onChange={(sql) => update({ sql })}
                dialect={kind}
                {...(completion ? { schema: completion } : {})}
                onRun={submit}
              />
            </Suspense>

            <div className="flex items-center gap-3">
              <Button type="submit" disabled={run.isPending || !active.sql.trim()}>
                {run.isPending ? t("editor.running") : t("editor.run")}
              </Button>

              <ShareButton sql={active.sql} connectionId={selected} title={active.title} />

              {run.data ? <ResultSummary result={run.data} /> : null}

              {/*
                Said once, where somebody is about to wonder why nothing
                completes. An editor that silently offers no table names looks
                broken; one that says the catalog has not been read explains
                itself and names the fix.
              */}
              {schema.data && !schema.data.synced ? (
                <span className="text-sm text-content-subtle">
                  {t("editor.notSynced")}
                </span>
              ) : null}
            </div>
          </form>
        </CardBody>
      </Card>

      {run.error ? <QueryFailure error={run.error} sql={active.sql} /> : null}
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
function QueryFailure({ error, sql }: { error: unknown; sql: string }) {
  const { t } = useTranslation();

  const message = error instanceof ApiError ? error.message : String(error);
  const at = error instanceof ApiError ? locate(sql, error.position) : undefined;

  return (
    <Alert tone="danger" title={t("editor.failed")}>
      {at ? (
        <p className="text-sm">
          {t("editor.atLine", { line: at.line, column: at.column })}
        </p>
      ) : null}

      <p className="font-mono text-sm">{message}</p>

      {/*
        The line itself, with the offending column marked. A position in the
        prose ("at line 3, column 12") still makes somebody count; showing the
        line and pointing at it does not.
      */}
      {at ? (
        <pre className="mt-2 overflow-auto font-mono text-xs">
          {at.text}
          {"\n"}
          {" ".repeat(Math.max(0, at.column - 1))}^
        </pre>
      ) : null}
    </Alert>
  );
}

/**
 * Turn the source's byte offset into a line, a column, and that line's text.
 *
 * Counted in bytes, because that is what the source reported. Doing it in
 * JavaScript string indices would land on the wrong character in any statement
 * containing a non-ASCII identifier or literal -- and a marker pointing one
 * place to the left of the problem is worse than no marker, because it is
 * confidently wrong.
 *
 * Returns undefined when there is no position, which is the ordinary case:
 * only PostgreSQL reports one at all.
 */
export function locate(
  sql: string,
  position: number | undefined,
): { line: number; column: number; text: string } | undefined {
  if (!position || position < 1) return undefined;

  const bytes = new TextEncoder().encode(sql);
  if (position > bytes.length) return undefined;

  // The source's offset is 1-based and points *at* the character.
  const upTo = new TextDecoder().decode(bytes.slice(0, position - 1));

  const lines = upTo.split("\n");
  const line = lines.length;
  const column = (lines[lines.length - 1] ?? "").length + 1;

  const text = sql.split("\n")[line - 1] ?? "";

  return { line, column, text };
}

function ResultSummary({ result }: { result: QueryResult }) {
  const { t } = useTranslation();

  return (
    <div className="flex items-center gap-2 text-sm text-content-muted">
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
    <Suspense fallback={<Skeleton className="h-64 w-full" />}>
      <ResultGrid columns={result.columns} rows={result.rows} />
    </Suspense>
  );
}

/*
 * The tab strip.
 *
 * Deliberately plain, and deliberately not the design system's Tabs: those are
 * for switching between views of one thing, where the set is fixed and named
 * by the author. These are documents -- opened, closed and renamed by whoever
 * is typing -- and giving them the same control would mean a keyboard user
 * hearing "tab 3 of 3" about something they can delete.
 */
function Tabs({
  tabs,
  activeId,
  onSelect,
  onOpen,
  onClose,
}: {
  tabs: EditorTab[];
  activeId: string;
  onSelect: (id: string) => void;
  onOpen: () => void;
  onClose: (id: string) => void;
}) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-wrap items-center gap-1" role="group" aria-label={t("editor.tabs")}>
      {tabs.map((tab) => (
        <span
          key={tab.id}
          className={`flex items-center gap-1 rounded-md border px-2 py-1 text-sm ${
            tab.id === activeId
              ? "border-line-strong bg-surface-sunken"
              : "border-transparent"
          }`}
        >
          <button type="button" onClick={() => onSelect(tab.id)} aria-current={tab.id === activeId}>
            {tab.title}
          </button>

          <button
            type="button"
            aria-label={t("editor.closeTab", { title: tab.title })}
            className="text-content-subtle hover:text-content"
            onClick={() => onClose(tab.id)}
          >
            ×
          </button>
        </span>
      ))}

      <Button type="button" variant="ghost" size="sm" onClick={onOpen}>
        {t("editor.newTab")}
      </Button>
    </div>
  );
}

/*
 * The shape CodeMirror completes from: table name to column names.
 *
 * Qualified with the schema only where there is more than one, because
 * "public.orders" is noise in a database that has only public, and the whole
 * value of completion is that it is shorter than typing.
 *
 * Returns undefined rather than an empty object when there is nothing, so the
 * editor omits the option entirely and CodeMirror keeps its own default.
 */
function completionSchema(
  schema: ConnectionSchema | undefined,
): Record<string, string[]> | undefined {
  if (!schema || schema.tables.length === 0) return undefined;

  const schemas = new Set(schema.tables.map((table) => table.schema));
  const qualify = schemas.size > 1;

  const out: Record<string, string[]> = {};

  for (const table of schema.tables) {
    const name = qualify && table.schema ? `${table.schema}.${table.name}` : table.name;
    out[name] = table.columns;
  }

  return out;
}

/**
 * Copy a link to this question.
 *
 * The link carries the source and the statement in the URL *fragment*, which
 * browsers never send to a server -- so the SQL stays out of access logs,
 * proxy logs and the Referer header of every subsequent request. A statement
 * can name tables, columns and filter values that are themselves sensitive.
 *
 * It carries the question and not the answer: whoever opens it still has to be
 * signed in, still needs the permission, and still needs access to that
 * connection. Sharing a link is not sharing data, and the button says so.
 */
function ShareButton({
  sql,
  connectionId,
  title,
}: {
  sql: string;
  connectionId: string;
  title: string;
}) {
  const { t } = useTranslation();
  const [state, setState] = useState<"idle" | "copied" | "tooLong">("idle");

  const link = questionLink({ connectionId, sql, title }, window.location.origin);
  const disabled = !connectionId || sql.trim() === "";

  function share() {
    if (link.length > MAX_LINK_LENGTH) {
      setState("tooLong");

      return;
    }

    void navigator.clipboard
      ?.writeText(link)
      .then(() => setState("copied"))
      .catch(() => setState("idle"));
  }

  useEffect(() => {
    if (state !== "copied") return undefined;

    const timer = window.setTimeout(() => setState("idle"), 2000);

    return () => window.clearTimeout(timer);
  }, [state]);

  return (
    <span className="flex items-center gap-2">
      <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={share}>
        {state === "copied" ? t("editor.linkCopied") : t("editor.share")}
      </Button>

      {state === "tooLong" ? (
        <span className="text-xs text-content-muted">{t("editor.linkTooLong")}</span>
      ) : null}
    </span>
  );
}
