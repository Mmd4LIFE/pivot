import { Link, createRoute } from "@tanstack/react-router";
import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { MAX_LINK_LENGTH, decodeQuestion, encodeQuestion, questionLink } from "./editor.link";
import { useSplit } from "./editor.split";
import { useWorkspace, type EditorTab } from "./editor.workspace";
import { ApiError, type ConnectionSchema, type QueryResult } from "../api/client";
import * as Glyph from "../components/editor/icons";
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

import type { SqlEditorApi } from "../components/editor/SqlEditor";

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

  const { workspace, active, update, open, close, select, openWith, rename, move } =
    useWorkspace();

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
  const split = useSplit();

  /*
   * What is selected in the editor, and the handle to the editor itself.
   *
   * The selection is lifted because the Run button has to say what it will do.
   * "Run selection" when there is one is the difference between a button
   * somebody trusts and one whose result they check afterwards.
   */
  const [selectedText, setSelectedText] = useState("");
  const editor = useRef<SqlEditorApi | null>(null);

  const statement = selectedText.trim() || active.sql;

  /*
   * The address bar *is* the link.
   *
   * Metabase keeps the question in the URL as you work, and that is the better
   * design: the thing people reach for when they want to share something is
   * the address bar, not a button they have to find. A Copy link button that
   * produced a URL different from the one on screen was the worst of both --
   * two sources of truth for the same question.
   *
   * replaceState rather than pushState, so typing does not fill the back
   * button with every intermediate state of a query.
   *
   * Debounced, because this runs on every keystroke and a history write per
   * character is work the browser does not need.
   */
  useEffect(() => {
    if (!selected || !active.sql.trim()) return undefined;

    const timer = window.setTimeout(() => {
      const fragment = encodeQuestion({
        connectionId: selected,
        sql: active.sql,
        title: active.title,
      });

      window.history.replaceState(null, "", `${window.location.pathname}#${fragment}`);
    }, 400);

    return () => window.clearTimeout(timer);
  }, [active.sql, active.title, selected]);
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
    if (!selected || !statement.trim()) return;

    run.mutate({ connectionId: selected, sql: statement });
  }

  /*
   * Format, loaded when somebody asks for it.
   *
   * A dynamic import inside the handler rather than at the top of the module:
   * the formatter is a dependency most sessions never touch, and the editor
   * chunk is already the largest thing this page fetches.
   *
   * Replaced as one transaction so a single undo puts back exactly what was
   * there. Reformatting somebody's query is only safe if it is trivially
   * reversible.
   */
  const format = useCallback(async () => {
    if (!active.sql.trim()) return;

    try {
      const { format: formatSQL } = await import("sql-formatter");

      const language = formatterLanguage(kind);

      editor.current?.replaceAll(
        formatSQL(active.sql, { language, keywordCase: "upper", tabWidth: 2 }),
      );
    } catch {
      // A statement the formatter cannot parse is left exactly as it is.
      // Refusing to format is a far better answer than mangling something
      // somebody is midway through writing.
    }
  }, [active.sql, kind]);

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
        onRename={rename}
        onMove={move}
      />

      <Card>
        <CardBody>
          <form className="flex flex-col gap-3" onSubmit={onSubmit}>
            {/*
              The source at the top; everything you do to the statement at the
              bottom right, under your hand.

              That is Metabase's arrangement and it is the right one. Which
              database you are asking is a decision made once, when you start;
              Run, Format and Copy link are pressed over and over while you
              work, and they belong at the end of the text rather than above
              it. A single row of controls that mixed the two put the source
              picker next to Run, where a mis-click changes which database a
              statement is about to hit.
            */}
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <label className="text-sm text-content-muted" htmlFor="editor-connection">
                  {t("editor.source")}
                </label>

                <select
                  id="editor-connection"
                  className="h-9 rounded-token-sm border border-line bg-surface px-2 text-sm"
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

              {/*
                Said once, beside the source it is about. An editor that
                silently offers no table names looks broken; one that says the
                catalog has not been read explains itself and names the fix.
              */}
              {schema.data && !schema.data.synced ? (
                <span className="text-sm text-content-subtle">{t("editor.notSynced")}</span>
              ) : null}
            </div>

            <Suspense fallback={<Skeleton className="h-40 w-full" />}>
              <SqlEditor
                value={active.sql}
                onChange={(sql) => update({ sql })}
                dialect={kind}
                {...(completion ? { schema: completion } : {})}
                onRun={submit}
                onSelectionChange={setSelectedText}
                onReady={(api) => {
                  editor.current = api;
                }}
                height={split.height}
              />
            </Suspense>

            <div className="flex flex-wrap items-center justify-between gap-3">
              {/*
                What the last run produced, on the left. It answers the
                question the Run button just asked, so it sits at the start of
                the line rather than trailing the controls.
              */}
              <div className="min-w-0">{run.data ? <ResultSummary result={run.data} /> : null}</div>

              {/*
                Icons, not words, for the three secondary actions.

                Four labelled buttons in a row give none of them emphasis, and
                the one that matters here is Run. Each carries an aria-label
                and a title, so the name is a hover away for a pointer and
                always present for a screen reader -- the thing an icon-only
                toolbar usually gets wrong.
              */}
              <div className="flex items-center gap-1">
                {/*
                  A link, not a button, because it is navigation: middle-click
                  opens the catalog in a tab beside the query somebody is
                  midway through writing, which is exactly how it gets used.
                */}
                <Link
                  to="/browse/$connectionId"
                  params={{ connectionId: selected }}
                  className={ICON_BUTTON}
                  aria-label={t("editor.catalog")}
                  title={t("editor.catalog")}
                >
                  <Glyph.Table />
                </Link>

                <IconButton
                  label={t("editor.format")}
                  disabled={!active.sql.trim()}
                  onClick={() => void format()}
                >
                  <Glyph.Format />
                </IconButton>

                <ShareButton sql={active.sql} connectionId={selected} title={active.title} />

                <Button
                  type="submit"
                  className="ml-1"
                  loading={run.isPending}
                  disabled={run.isPending || !statement.trim()}
                  title={t("editor.runHint")}
                >
                  {run.isPending ? null : <Glyph.Play />}

                  {run.isPending
                    ? t("editor.running")
                    : selectedText.trim()
                      ? t("editor.runSelection")
                      : t("editor.run")}
                </Button>
              </div>
            </div>

            {/*
              The grip last, because it is the boundary between this pane and
              the results below it -- not a divider inside the pane.
            */}
            <Splitter height={split.height} onDrag={split.startDrag} onNudge={split.nudge} />
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
  onRename,
  onMove,
}: {
  tabs: EditorTab[];
  activeId: string;
  onSelect: (id: string) => void;
  onOpen: () => void;
  onClose: (id: string) => void;
  onRename: (id: string, title: string) => void;
  onMove: (id: string, direction: -1 | 1) => void;
}) {
  const { t } = useTranslation();
  const [renaming, setRenaming] = useState<string | null>(null);

  return (
    <div className="flex flex-wrap items-center gap-1" role="group" aria-label={t("editor.tabs")}>
      {tabs.map((tab, index) => (
        <span
          key={tab.id}
          className={`flex items-center gap-1 rounded-token-sm border px-2 py-1 text-sm ${
            tab.id === activeId ? "border-line-strong bg-surface-sunken" : "border-transparent"
          }`}
        >
          {/*
            Reorder by button, not by drag. A reorder somebody can reach with a
            keyboard is worth more than one that looks better with a mouse, and
            drag-and-drop without a keyboard equivalent excludes people. Drag
            can be layered on top later; it cannot be retrofitted underneath.
          */}
          {tab.id === activeId && tabs.length > 1 ? (
            <button
              type="button"
              aria-label={t("editor.moveLeft", { title: tab.title })}
              disabled={index === 0}
              onClick={() => onMove(tab.id, -1)}
              className="text-content-subtle hover:text-content disabled:opacity-30"
            >
              ‹
            </button>
          ) : null}

          {renaming === tab.id ? (
            <input
              autoFocus
              aria-label={t("editor.renameTab", { title: tab.title })}
              defaultValue={tab.title}
              onBlur={(event) => {
                onRename(tab.id, event.target.value);
                setRenaming(null);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter") event.currentTarget.blur();

                // Escape abandons the rename, which is what it means
                // everywhere else and what somebody expects after a mistype.
                if (event.key === "Escape") {
                  event.currentTarget.value = tab.title;
                  event.currentTarget.blur();
                }
              }}
              className="w-24 rounded-token-sm border border-line bg-surface px-1"
            />
          ) : (
            <button
              type="button"
              onClick={() => (tab.id === activeId ? setRenaming(tab.id) : onSelect(tab.id))}
              aria-current={tab.id === activeId}
              title={tab.id === activeId ? t("editor.renameHint") : tab.title}
            >
              {tab.title}
            </button>
          )}

          {tab.id === activeId && tabs.length > 1 ? (
            <button
              type="button"
              aria-label={t("editor.moveRight", { title: tab.title })}
              disabled={index === tabs.length - 1}
              onClick={() => onMove(tab.id, 1)}
              className="text-content-subtle hover:text-content disabled:opacity-30"
            >
              ›
            </button>
          ) : null}

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
 * One toolbar action.
 *
 * The shared look, and the two attributes an icon-only control cannot do
 * without: `aria-label` names it for a screen reader and `title` names it on
 * hover. Icons without either are the usual way a compact toolbar becomes a
 * guessing game.
 */
const ICON_BUTTON =
  "inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-token text-content-muted transition-colors hover:bg-surface-sunken hover:text-content focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:pointer-events-none disabled:opacity-40";

function IconButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      className={ICON_BUTTON}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
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
      {/*
        The refusal is spoken, not just drawn: a button that declines to copy
        and gives no reason reads as a broken button.
      */}
      {state === "tooLong" ? (
        <span className="text-xs text-content-muted">{t("editor.linkTooLong")}</span>
      ) : null}

      <IconButton
        label={state === "copied" ? t("editor.linkCopied") : t("editor.share")}
        disabled={disabled}
        onClick={share}
      >
        {state === "copied" ? <Glyph.Check /> : <Glyph.Link />}
      </IconButton>
    </span>
  );
}

/**
 * The grip between the editor and the results.
 *
 * A separator with a role and a value, not a bare div: the ARIA pattern asks
 * for arrow keys, and a splitter that only answers a pointer is a layout
 * somebody using a keyboard cannot change at all.
 */
function Splitter({
  height,
  onDrag,
  onNudge,
}: {
  height: number;
  onDrag: (event: { clientY: number; preventDefault: () => void }) => void;
  onNudge: (delta: number) => void;
}) {
  const { t } = useTranslation();

  return (
    <div
      role="separator"
      aria-orientation="horizontal"
      aria-label={t("editor.resize")}
      aria-valuenow={height}
      tabIndex={0}
      onPointerDown={onDrag}
      onKeyDown={(event) => {
        if (event.key === "ArrowUp") {
          event.preventDefault();
          onNudge(-24);
        }

        if (event.key === "ArrowDown") {
          event.preventDefault();
          onNudge(24);
        }
      }}
      className="group -my-1 flex h-3 cursor-row-resize items-center justify-center focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
    >
      <span className="h-0.5 w-10 rounded-full bg-line-strong transition-colors group-hover:bg-accent" />
    </div>
  );
}

/**
 * Which dialect the formatter should parse as.
 *
 * Falls back to standard SQL rather than guessing, because a formatter given
 * the wrong dialect mangles the syntax that dialect has and the other does not
 * -- and a Format button that damages a query is one nobody presses twice.
 */
function formatterLanguage(kind: string): "postgresql" | "mysql" | "sqlite" | "sql" {
  switch (kind) {
    case "postgres":
      return "postgresql";
    case "mysql":
      return "mysql";
    case "sqlite":
      return "sqlite";
    default:
      return "sql";
  }
}
