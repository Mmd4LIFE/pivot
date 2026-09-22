# Keyboard audit

**Automated layers:** green as of 2026-09-22 (Part 10-b).
**Manual pass:** *not yet run.* The checklist at the end of this file is what
Part 10-b produced; nobody has worked through it with a keyboard and a screen
reader. Record the date and the result here when they have.

Every component in `web/src/ui`, and what is known about using it without a
mouse. This file exists because "Radix handles it" is true until somebody adds
a `stopPropagation` to fix an unrelated bug, and because some of what matters
here cannot be checked by a machine that has no screen.

There are four layers, and each one catches what the others cannot:

| Layer | Where | What it can see |
|---|---|---|
| Contrast | `web/tokens_test.go` (Go) | Whether a color pair is legible. Runs anywhere, including with no `node_modules`. |
| Structure | `web/src/ui/a11y.test.tsx` (axe in jsdom) | Names, roles, states, ARIA validity. 106 story scans. |
| Behavior | `web/src/ui/keyboard.test.tsx` (user-event in jsdom) | Focus movement, focus trapping, Escape, arrow keys, live-region politeness. 21 tests. |
| Rendering | **this file**, by hand | Whether the focus ring is actually *drawn*, where the overlay lands, what a screen reader says out loud. |

jsdom builds a DOM and never lays it out. It has no painted pixels, so nothing
automated in this repository can tell you that a focus ring is visible — only
that the element which should have focus does. The fourth row is the gap, and
Part 11's Playwright install is what starts closing it.

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

## What still needs a person

Run `make storybook`, open <http://localhost:6006>, and work through this with
the mouse pushed out of reach. Record the date and the result above.

Nothing here is checkable by the test suite, because all of it is about what
appears on a screen or what is said out loud.

### Every component

1. **Is the focus ring visible?** Tab to each control in each story, in both
   themes. The ring is a 2px accent outline with a 2px offset, from
   `:focus-visible` in `app.css`. `web/tokens_test.go` proves the color clears
   3:1 against both surfaces; it cannot prove that anything drew it. Watch in
   particular for a ring clipped by an `overflow: hidden` ancestor — a real and
   common failure that looks like no ring at all.
2. **Does the ring appear only on keyboard focus?** `:focus-visible`, not
   `:focus`: clicking a button should not leave a ring behind it.

### Overlays

3. **Where does it land?** jsdom cannot position anything, so every overlay's
   placement is unverified by the suite. Check that a Select near the bottom of
   the viewport flips above its trigger, and that a Tooltip at the right edge
   does not run off-screen.
4. **Is the page behind locked?** Open a Dialog and scroll. The page behind
   should not move.
5. **Submenu on the keyboard.** ArrowRight opens a DropdownMenu submenu,
   ArrowLeft closes it. Not covered by a test.
6. **Select typeahead.** With the list open, type `ad` and check that Admin is
   highlighted.

### Screen reader

7. With VoiceOver, NVDA or Orca: open the command palette and arrow through the
   list. **Each option should be read aloud as it becomes active.** This is the
   bug above; the fix is asserted in the DOM but has never been heard.
8. Trigger each Toast tone. A normal toast should be read at the next pause; an
   error should interrupt. Neither should move the reading cursor.
9. Open a Dialog and try to read the page behind it. Nothing outside should be
   reachable.

### Right to left

10. Set `dir="rtl"` on `<html>` in the inspector. Menus, the Select chevron,
    the Switch thumb and the Dialog's close button should all mirror. Only the
    Switch has a story for this today.
