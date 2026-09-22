# Keyboard audit

**Automated layers:** green as of 2026-09-22 (Part 11-b).
**Manual pass:** *partly closed by machine, the rest not yet run.* Part 11-b
added a browser, which turned four of the ten items below into tests. The
remaining six need a person; nobody has worked through them. Record the date
and the result here when they have.

Every component in `web/src/ui`, and what is known about using it without a
mouse. This file exists because "Radix handles it" is true until somebody adds
a `stopPropagation` to fix an unrelated bug, and because some of what matters
here cannot be checked by a machine that has no screen.

There are five layers, and each one catches what the others cannot:

| Layer | Where | What it can see |
|---|---|---|
| Contrast, by intent | `web/tokens_test.go` (Go) | Whether a token pair *would* be legible. Runs anywhere, including with no `node_modules`. |
| Structure | `web/src/ui/a11y.test.tsx` (axe in jsdom) | Names, roles, states, ARIA validity. 106 story scans. |
| Behavior | `web/src/ui/keyboard.test.tsx` (user-event in jsdom) | Focus movement, focus trapping, Escape, arrow keys, live-region politeness. 21 tests. |
| Contrast, as painted | `web/e2e/a11y.spec.ts` (axe in Chromium) | What the browser actually rendered, in both themes, including overlays. 16 scans. |
| The rest | **this file**, by hand | What a screen reader says out loud, where an overlay lands, whether the page behind a dialog is locked. |

The fourth row is new in Part 11-b and it matters more than its size suggests.
jsdom lays nothing out and has no canvas, so axe's `color-contrast` rule does
not run there at all — the jsdom suite disables it explicitly. In a browser it
runs, and on its first outing it found **two real failures that the Go harness
had no pairing for**: an avatar's initials, and an accent badge in the dark
theme. A list of token pairs is only as good as the combinations somebody
thought of.

---

## What is proven by test

These are asserted in `keyboard.test.tsx` and fail the build if they regress.
Each was mutation-verified: the assertion was watched to fail against a
deliberately broken version before being kept.

### Dialog

- Reachable by Tab; opens on Enter from its trigger.
- Focus moves inside on open.
- **Focus is trapped**: eight consecutive Tabs never leave the dialog.
- Escape closes it, **and focus returns to the trigger**. This is the one
  people forget; without it the next Tab restarts at the top of the document
  and a keyboard user has lost their place entirely.
- Everything outside is `aria-hidden` while it is open, so a screen reader
  cannot read the page behind a modal.

### DropdownMenu

- ArrowDown opens it with the first item focused.
- ArrowDown moves between items.
- Escape closes it and focus returns to the trigger.

### Select

- The trigger is labeled by its `Field` — checked explicitly, because a Select
  trigger is a `<button>` and does not get the label association a native
  `<input>` gets for free.
- Enter opens, Escape closes, focus returns to the trigger.
- A value can be chosen with ArrowDown and Enter, with no pointer at any point.

### Popover

- Takes focus on open — this is the line between a Popover and a Tooltip, and
  it is why anything interactive belongs in the former.
- Escape closes it and focus returns to the trigger.

### Tabs

- The whole strip is **one** tab stop: one Tab reaches it, the next leaves it.
  Asserted behaviorally rather than by reading `tabindex`, because Radix puts
  the stop on the list and forwards focus to the selected tab.
- ArrowRight moves to the next tab and selects it.

### Toast

- `role="status"`, never `role="alert"`, in every tone.
- A normal toast is `aria-live="polite"`; an error is `assertive`.
- **Focus does not move** when one appears.

### Command palette

- Opens on both `Ctrl+K` and `Cmd+K`, because a product used on two platforms
  cannot ask people to learn which one this build assumed.
- Escape closes it.
- Typing filters; focus stays in the input throughout.
- `aria-activedescendant` on the input names the active option — see the bug
  below.

### In-flow controls

- Every control in a form is reached by Tab in source order.
- A disabled control is skipped.
- A loading button keeps its place in the tab order (`aria-busy`, not
  `disabled`-and-gone).

---

## One real bug, found and fixed

**cmdk sets `aria-activedescendant` only when the selection changes.**

So it is absent when the palette opens — the first option is highlighted and
nothing changed — and absent again whenever a search narrows to a single
result, because the selection stays where it was. Both are precisely the
moments a screen reader user needs to be told what is active, and in both of
them the highlight is plainly visible to everyone else.

Reproduced against cmdk on its own, so it is not something this wrapper broke.
axe does not catch it: an absent attribute is not an invalid one. Fixed in
`CommandPalette.tsx` (`useActiveDescendant`) by mirroring the DOM's
`aria-selected` onto the input, and pinned by a test that was watched to fail
without the fix.

---

## Closed by the browser in Part 11-b

These were on the manual list and are now tests. They are here so nobody spends
twenty minutes re-checking them by hand.

- **Contrast, as painted, in both themes.** `web/e2e/a11y.spec.ts` runs axe over
  six pages plus the login page, the command palette and the account menu, in
  light and dark. This is the check jsdom cannot make.
- **The right-to-left layout genuinely mirrors.** The sidebar's bounding box is
  measured on both sides of a `dir="rtl"` switch — real layout, not an
  attribute.
- **The skip link is the first tab stop, is invisible until focused, is visible
  once focused, and moves focus into `<main>`.**
- **One `<main>`, one `<h1>`, on every page.**

## What still needs a person

Six items. Run `make storybook` for the components and `make all &&
./bin/pivot serve` for the application, and work through this with the mouse
pushed out of reach. Record the date and the result at the top of this file.

### Rendering

1. **Is the focus ring visible on every control?** Tab through each story in
   both themes. The ring is a 2px accent outline with a 2px offset, from
   `:focus-visible` in `app.css`. `web/tokens_test.go` proves the color clears
   3:1 and axe now proves the surrounding text is legible, but neither can tell
   you the ring was *drawn*. Watch for one clipped by an `overflow: hidden`
   ancestor — a real and common failure that looks like no ring at all.
2. **Does the ring appear only on keyboard focus?** `:focus-visible`, not
   `:focus`: clicking a button should not leave a ring behind it.

### Overlays

3. **Where does it land?** Check that a Select near the bottom of the viewport
   flips above its trigger, and that a Tooltip at the right edge does not run
   off-screen. Playwright could assert this; nothing does yet.
4. **Is the page behind locked?** Open a Dialog and scroll. The page behind
   should not move.
5. **Submenu and typeahead on the keyboard.** ArrowRight opens a DropdownMenu
   submenu and ArrowLeft closes it; with a Select open, typing `ad` highlights
   Admin.

### Screen reader

6. With VoiceOver, NVDA or Orca:
   - Open the command palette and arrow through the list. **Each option should
     be read aloud as it becomes active.** The `aria-activedescendant` fix is
     asserted in the DOM and has never been heard.
   - Trigger each Toast tone. A normal one should be read at the next pause; an
     error should interrupt. Neither should move the reading cursor.
   - Open a Dialog and try to read the page behind it. Nothing outside should
     be reachable.
