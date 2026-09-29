import {
  sql,
  SQLite,
  PostgreSQL,
  MySQL,
  type SQLConfig,
  type SQLDialect,
} from "@codemirror/lang-sql";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, keymap, lineNumbers, placeholder } from "@codemirror/view";
import { useEffect, useRef } from "react";

/**
 * The SQL editor itself.
 *
 * In its own module because it is the only thing in Pivot that imports
 * CodeMirror, and CodeMirror is the largest dependency this product takes. The
 * route loads it with a dynamic import, so it becomes a chunk a browser fetches
 * when somebody opens the editor rather than bytes every visit pays for --
 * which is what keeps the 200 KB initial budget intact. Importing this module
 * eagerly from anywhere undoes that silently, and the bundle gate is what would
 * catch it.
 *
 * Uncontrolled on purpose. A controlled CodeMirror -- rebuilding state on every
 * keystroke -- loses the cursor, the undo history and the selection, which is
 * most of what an editor is. The document is pushed in only when it changes
 * underneath us, which is what switching tabs does.
 */

export interface SqlEditorProps {
  value: string;
  onChange: (value: string) => void;

  /** Which dialect to highlight and complete for. */
  dialect: string;

  /** Tables and their columns, for completion. Empty until a catalog sync. */
  schema?: Record<string, string[]>;

  /** Run the statement. Wired to Ctrl/Cmd-Enter, which is what people try. */
  onRun?: () => void;
}

/**
 * The dialect CodeMirror highlights and completes with.
 *
 * Falls back to standard SQL rather than guessing, because a wrong dialect is
 * worse than a generic one: it highlights valid syntax as an error and offers
 * keywords the source will reject.
 */
function dialectFor(kind: string): SQLDialect | undefined {
  switch (kind) {
    case "postgres":
      return PostgreSQL;
    case "mysql":
      return MySQL;
    case "sqlite":
    case "duckdb":
      return SQLite;
    default:
      return undefined;
  }
}

/*
 * The language configuration, assembled rather than spread.
 *
 * The project compiles with exactOptionalPropertyTypes, where passing
 * `dialect: undefined` is not the same as omitting the key -- and omitting it
 * is what should happen for a source whose dialect CodeMirror does not know,
 * so that its own standard-SQL default applies.
 */
function sqlConfig(dialect: string, schema?: Record<string, string[]>): SQLConfig {
  const config: SQLConfig = { upperCaseKeywords: true };

  const known = dialectFor(dialect);
  if (known) config.dialect = known;

  if (schema) config.schema = schema;

  return config;
}

export function SqlEditor({ value, onChange, dialect, schema, onRun }: SqlEditorProps) {
  const host = useRef<HTMLDivElement | null>(null);
  const view = useRef<EditorView | null>(null);

  // The callbacks live in refs so that changing one does not rebuild the
  // editor, which would take the cursor and the undo history with it.
  const change = useRef(onChange);
  const run = useRef(onRun);
  change.current = onChange;
  run.current = onRun;

  useEffect(() => {
    if (!host.current) return undefined;

    const extensions: Extension[] = [
      lineNumbers(),
      history(),
      placeholder("SELECT …"),
      sql(sqlConfig(dialect, schema)),
      keymap.of([
        // Before the defaults, so Ctrl-Enter runs rather than inserting a
        // newline. People try this before they try a button.
        {
          key: "Mod-Enter",
          preventDefault: true,
          run: () => {
            run.current?.();
            return true;
          },
        },
        indentWithTab,
        ...defaultKeymap,
        ...historyKeymap,
      ]),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) change.current(update.state.doc.toString());
      }),
      EditorView.theme({
        "&": { fontSize: "13px" },
        ".cm-content": { fontFamily: "ui-monospace, SFMono-Regular, monospace" },
        "&.cm-focused": { outline: "none" },
      }),
    ];

    const editor = new EditorView({
      state: EditorState.create({ doc: value, extensions }),
      parent: host.current,
    });

    view.current = editor;

    return () => {
      editor.destroy();
      view.current = null;
    };

    // Rebuilt when the dialect or the schema changes, because both are baked
    // into the extension. Deliberately not on `value` -- see the note about
    // being uncontrolled.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dialect, schema]);

  // A document that changed underneath the editor -- switching tabs -- is
  // pushed in. One that already matches is left alone, which is what keeps
  // typing from fighting its own state.
  useEffect(() => {
    const editor = view.current;
    if (!editor) return;

    if (editor.state.doc.toString() !== value) {
      editor.dispatch({
        changes: { from: 0, to: editor.state.doc.length, insert: value },
      });
    }
  }, [value]);

  return (
    <div
      ref={host}
      data-testid="sql-editor"
      className="min-h-40 overflow-auto rounded-md border border-[--color-border] bg-[--color-bg]"
    />
  );
}

export default SqlEditor;
