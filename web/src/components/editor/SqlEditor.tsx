import {
  sql,
  SQLite,
  PostgreSQL,
  MySQL,
  type SQLConfig,
  type SQLDialect,
} from "@codemirror/lang-sql";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import {
  EditorView,
  drawSelection,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
  placeholder,
  rectangularSelection,
} from "@codemirror/view";
import { bracketMatching, indentOnInput } from "@codemirror/language";
import {
  acceptCompletion,
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completionKeymap,
} from "@codemirror/autocomplete";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { useEffect, useRef } from "react";

import { pivotEditorTheme, pivotHighlight } from "./theme";

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

  /*
   * The language, in a compartment.
   *
   * Changing the dialect or the schema has to reconfigure the editor rather
   * than rebuild it. A rebuild throws away the cursor, the selection and the
   * undo history -- so switching a tab's connection would silently move
   * somebody's caret to the start of their query, which is the same bug as
   * the one below and just rarer.
   */
  const language = useRef(new Compartment());

  useEffect(() => {
    if (!host.current) return undefined;

    const extensions: Extension[] = [
      lineNumbers(),
      highlightActiveLine(),
      highlightActiveLineGutter(),
      history(),
      placeholder("SELECT …"),

      // Selection drawn by CodeMirror rather than the browser, so that the
      // theme can colour it and so rectangular selection works at all.
      drawSelection(),
      rectangularSelection(),

      bracketMatching(),
      closeBrackets(),
      indentOnInput(),
      highlightSelectionMatches(),

      // Search as a panel rather than the browser's find, which cannot see
      // text CodeMirror has not rendered.
      search({ top: true }),

      // Completion opens on typing rather than only on a keystroke somebody
      // has to know about. `activateOnTyping` is the difference between
      // autocomplete people use and autocomplete people are told exists.
      autocompletion({ activateOnTyping: true, icons: false }),

      pivotEditorTheme,
      pivotHighlight,
      language.current.of(sql(sqlConfig(dialect, schema))),
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
        /*
          Tab accepts the completion, and indents when there is not one.

          `acceptCompletion` returns false when no completion is open, so the
          binding falls through to indentWithTab below it -- which is why the
          order matters and why this is not two separate keys. Tab is what
          people press: every editor they have used accepts a suggestion with
          it, and leaving it bound only to indent means the suggestion has to
          be dismissed before the line can be indented anyway.
        */
        { key: "Tab", run: acceptCompletion },
        indentWithTab,
        ...completionKeymap,
        ...closeBracketsKeymap,
        ...searchKeymap,
        ...defaultKeymap,
        ...historyKeymap,
      ]),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) change.current(update.state.doc.toString());
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

    /*
     * Built exactly once, for the life of the component.
     *
     * The first version of this listed [dialect, schema], and the schema was
     * a fresh object on every render -- so every keystroke destroyed the
     * editor and built a new one, and the cursor vanished after each
     * character. Nothing about the language belongs in here now; the
     * compartment handles it.
     */
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // The language, reconfigured in place when the source or its schema changes.
  useEffect(() => {
    view.current?.dispatch({
      effects: language.current.reconfigure(sql(sqlConfig(dialect, schema))),
    });
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
      // A real height rather than a minimum, so the editor has somewhere to
      // fill. With min-height the box grows with the content and the empty
      // area below a short query belongs to nothing.
      className="h-56 overflow-hidden rounded-token border border-line"
    />
  );
}

export default SqlEditor;
