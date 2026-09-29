import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags } from "@lezer/highlight";
import type { Extension } from "@codemirror/state";

/**
 * CodeMirror, wearing Pivot's own colours.
 *
 * Every value here is `rgb(var(--pivot-…))` rather than a hex, and that is the
 * load-bearing rule rather than tidiness. An embedder restyles Pivot by
 * redefining those custom properties (Phase 8's white-label embedding is a
 * configuration change, not a rebuild), and one hard-coded colour in here is a
 * patch of somebody else's product that does not follow their brand -- which
 * nobody notices until it ships.
 *
 * It is also why there is one theme rather than a light one and a dark one.
 * The tokens already change with the mode; a theme built on them changes with
 * it too, and cannot drift from the rest of the interface the way two hand-
 * maintained palettes do.
 *
 * Part 23-b shipped no theme at all -- the components referenced `--color-*`
 * properties that do not exist here, so the editor rendered with the browser's
 * defaults in both modes. This is what it should have been.
 */

/** A token, with an alpha modifier where a wash is wanted rather than a fill. */
function token(name: string, alpha?: number): string {
  return alpha === undefined ? `rgb(var(--pivot-${name}))` : `rgb(var(--pivot-${name}) / ${alpha})`;
}

export const pivotEditorTheme: Extension = EditorView.theme({
  /*
    The editor fills its container, and that is what makes clicking empty space
    work.

    CodeMirror sizes itself to its content by default, so the area below a
    short query belongs to the wrapping div and a click there lands on nothing
    -- somebody aiming at the end of their query has to hit the last line
    exactly. Making the editor, its scroller and its content fill the height
    means the whole box is the editor, and a click anywhere in it puts the
    cursor on the nearest line.
  */
  "&": {
    height: "100%",
    color: token("text"),
    backgroundColor: token("surface"),
    fontSize: "13px",
    borderRadius: "var(--pivot-radius)",
  },

  ".cm-scroller": { overflow: "auto" },

  ".cm-content": {
    fontFamily: "var(--pivot-font-mono)",
    padding: "10px 0",
    caretColor: token("accent"),

    // The content box, not just the text, so the click target is the whole
    // editor rather than the lines that happen to exist.
    minHeight: "100%",
  },

  // The caret is drawn by CodeMirror rather than the browser, so it needs
  // colouring in both places or it is invisible on one of them.
  "&.cm-focused .cm-cursor": { borderLeftColor: token("accent"), borderLeftWidth: "2px" },

  "&.cm-focused": { outline: "none" },

  /*
    Selection, at a wash rather than a fill.

    A solid accent behind selected text drops the contrast of the text itself
    below readable, which matters most when somebody has selected a line in
    order to run it.
  */
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
    backgroundColor: token("accent", 0.25),
  },

  ".cm-activeLine": { backgroundColor: token("surface-sunken", 0.6) },
  ".cm-activeLineGutter": { backgroundColor: token("surface-sunken"), color: token("text-muted") },

  ".cm-gutters": {
    backgroundColor: token("surface-sunken"),
    color: token("text-subtle"),
    border: "none",
    borderRight: `1px solid ${token("border")}`,
  },

  ".cm-lineNumbers .cm-gutterElement": { padding: "0 12px 0 8px", minWidth: "2.5rem" },

  // Bracket matching, which is the cheapest orientation aid there is in a
  // query somebody is midway through nesting.
  ".cm-matchingBracket, &.cm-focused .cm-matchingBracket": {
    backgroundColor: token("accent", 0.25),
    outline: `1px solid ${token("accent")}`,
  },
  ".cm-nonmatchingBracket": { outline: `1px solid ${token("danger")}` },

  /*
    The completion popup.

    Styled here rather than left to CodeMirror's default, which is a white box
    with a blue highlight -- correct nowhere in this product and actively wrong
    in dark mode, where it arrives as a bright rectangle over a dark page.
  */
  ".cm-tooltip": {
    backgroundColor: token("surface-raised"),
    border: `1px solid ${token("border")}`,
    borderRadius: "var(--pivot-radius)",
    boxShadow: "var(--pivot-shadow)",
    color: token("text"),
  },
  ".cm-tooltip.cm-tooltip-autocomplete > ul": {
    fontFamily: "var(--pivot-font-mono)",
    maxHeight: "16rem",
  },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li": { padding: "3px 10px" },
  ".cm-tooltip-autocomplete ul li[aria-selected]": {
    backgroundColor: token("accent"),
    color: token("accent-text"),
  },
  ".cm-completionLabel": { color: "inherit" },
  ".cm-completionDetail": { color: token("text-subtle"), fontStyle: "normal", marginLeft: "1rem" },

  ".cm-placeholder": { color: token("text-subtle") },

  // Search and replace, which arrives as a panel and is otherwise unstyled.
  ".cm-panels": {
    backgroundColor: token("surface-sunken"),
    color: token("text"),
    borderTop: `1px solid ${token("border")}`,
  },
  ".cm-panel input, .cm-panel button": {
    fontFamily: "var(--pivot-font-sans)",
    backgroundColor: token("surface"),
    color: token("text"),
    border: `1px solid ${token("border")}`,
    borderRadius: "var(--pivot-radius-sm)",
    padding: "2px 6px",
  },

  ".cm-gutter": { minHeight: "100%" },
});

/**
 * Syntax colours, from the semantic tokens.
 *
 * A deliberately small palette. SQL has four things worth telling apart at a
 * glance -- the keywords that give a statement its shape, the names it refers
 * to, the literals somebody typed, and the comments that are not code -- and a
 * highlighter that colours twenty token types makes a query look like
 * confetti without making any of it easier to read.
 */
export const pivotHighlight: Extension = syntaxHighlighting(
  HighlightStyle.define([
    { tag: [tags.keyword, tags.operatorKeyword], color: token("accent"), fontWeight: "600" },
    { tag: [tags.string, tags.special(tags.string)], color: token("success") },
    { tag: [tags.number, tags.bool, tags.null], color: token("info") },
    { tag: [tags.comment, tags.lineComment, tags.blockComment], color: token("text-subtle"), fontStyle: "italic" },
    { tag: [tags.typeName, tags.className], color: token("warning") },
    { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], color: token("info") },
    { tag: tags.invalid, color: token("danger") },
  ]),
);
