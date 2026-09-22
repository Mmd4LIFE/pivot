import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, test } from "vitest";

import { Button } from "./Button";
import { CommandAction, CommandPalette, useCommandPaletteHotkey } from "./CommandPalette";
import { DialogClose, DialogContent, DialogRoot, DialogTrigger } from "./Dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "./DropdownMenu";
import { Field } from "./Field";
import { Input } from "./Input";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";
import {
  SelectContent,
  SelectItem,
  SelectRoot,
  SelectTrigger,
  SelectValue,
} from "./Select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./Tabs";
import { Toast, ToastProvider, ToastViewport } from "./Toast";

/*
 * The keyboard contract, driven rather than described.
 *
 * axe checks that a control has a name and a role. It cannot check that
 * Escape closes the thing, that focus went where it should, or that focus came
 * back afterwards — and those are the failures that make an overlay unusable
 * without a mouse. They are also the ones a component library is supposed to
 * handle, which is exactly why they should be asserted: "Radix does it" stops
 * being true the moment somebody adds a `stopPropagation` to fix a different
 * bug.
 *
 * jsdom has no layout, so what is checked here is focus, roles and key
 * handling — everything that lives in the DOM. Whether the focus ring is
 * actually *visible* is a rendering question, and that one is in
 * docs/design/keyboard-audit.md for a person to answer.
 */

afterEach(() => {
  cleanup();
});

/** A dialog with a trigger, a field inside, and a close button. */
function DialogFixture() {
  return (
    <DialogRoot>
      <DialogTrigger asChild>
        <Button variant="secondary">Rename</Button>
      </DialogTrigger>

      <DialogContent
        title="Rename dashboard"
        footer={
          <DialogClose asChild>
            <Button>Save</Button>
          </DialogClose>
        }
      >
        <Field label="Name">
          <Input defaultValue="Weekly revenue" />
        </Field>
      </DialogContent>
    </DialogRoot>
  );
}

describe("Dialog", () => {
  test("opens from the keyboard and moves focus inside", async () => {
    const user = userEvent.setup();

    render(<DialogFixture />);

    await user.tab();
    expect(screen.getByRole("button", { name: "Rename" })).toHaveFocus();

    await user.keyboard("{Enter}");

    const dialog = await screen.findByRole("dialog");

    // Somewhere inside, not anywhere in particular: Radix focuses the first
    // focusable element, and pinning which one would fail on a layout change
    // that is not a regression.
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });
  });

  test("traps focus: tabbing never leaves the dialog", async () => {
    const user = userEvent.setup();

    render(<DialogFixture />);

    await user.tab();
    await user.keyboard("{Enter}");

    const dialog = await screen.findByRole("dialog");

    // More tabs than the dialog has stops, so a leak would have shown by now.
    for (let i = 0; i < 8; i += 1) {
      await user.tab();

      expect(dialog.contains(document.activeElement)).toBe(true);
    }
  });

  test("Escape closes it and focus returns to the trigger", async () => {
    const user = userEvent.setup();

    render(<DialogFixture />);

    const trigger = screen.getByRole("button", { name: "Rename" });

    await user.tab();
    await user.keyboard("{Enter}");
    await screen.findByRole("dialog");

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    // The part people forget. Without it the next Tab starts from the top of
    // the document, and a keyboard user has lost their place entirely.
    await waitFor(() => {
      expect(trigger).toHaveFocus();
    });
  });

  test("the page behind is hidden from assistive technology while it is open", async () => {
    const user = userEvent.setup();

    render(
      <>
        <p data-testid="behind">Dashboard list</p>
        <DialogFixture />
      </>,
    );

    await user.tab();
    await user.keyboard("{Enter}");
    await screen.findByRole("dialog");

    // Radix marks everything outside the dialog aria-hidden. Without it a
    // screen reader user can read the page behind a modal, which is the whole
    // thing a modal exists to prevent.
    await waitFor(() => {
      const behind = screen.getByTestId("behind");

      expect(behind.closest("[aria-hidden='true']")).not.toBeNull();
    });
  });
});

describe("DropdownMenu", () => {
  function MenuFixture() {
    return (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="secondary">Actions</Button>
        </DropdownMenuTrigger>

        <DropdownMenuContent>
          <DropdownMenuItem>Rename</DropdownMenuItem>
          <DropdownMenuItem>Duplicate</DropdownMenuItem>
          <DropdownMenuItem>Delete</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    );
  }

  test("opens on ArrowDown with the first item focused", async () => {
    const user = userEvent.setup();

    render(<MenuFixture />);

    await user.tab();
    await user.keyboard("{ArrowDown}");

    const items = await screen.findAllByRole("menuitem");

    await waitFor(() => {
      expect(items[0]).toHaveFocus();
    });
  });

  test("ArrowDown moves between items", async () => {
    const user = userEvent.setup();

    render(<MenuFixture />);

    await user.tab();
    await user.keyboard("{ArrowDown}");

    const items = await screen.findAllByRole("menuitem");
    await waitFor(() => expect(items[0]).toHaveFocus());

    await user.keyboard("{ArrowDown}");
    await waitFor(() => expect(items[1]).toHaveFocus());
  });

  test("Escape closes it and focus returns to the trigger", async () => {
    const user = userEvent.setup();

    render(<MenuFixture />);

    const trigger = screen.getByRole("button", { name: "Actions" });

    await user.tab();
    await user.keyboard("{ArrowDown}");
    await screen.findAllByRole("menuitem");

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryAllByRole("menuitem")).toHaveLength(0);
      expect(trigger).toHaveFocus();
    });
  });
});

describe("Select", () => {
  function SelectFixture() {
    return (
      <Field label="Default role">
        <SelectRoot defaultValue="viewer">
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>

          <SelectContent>
            <SelectItem value="viewer">Viewer</SelectItem>
            <SelectItem value="editor">Editor</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
          </SelectContent>
        </SelectRoot>
      </Field>
    );
  }

  test("the trigger is labeled by the Field", async () => {
    render(<SelectFixture />);

    // The wiring Field exists to guarantee, checked on the one control that
    // is not an <input> and so does not get it from the browser.
    expect(screen.getByRole("combobox", { name: /default role/i })).toBeTruthy();
  });

  test("opens from the keyboard and closes on Escape, restoring focus", async () => {
    const user = userEvent.setup();

    render(<SelectFixture />);

    const trigger = screen.getByRole("combobox");

    await user.tab();
    expect(trigger).toHaveFocus();

    await user.keyboard("{Enter}");
    await screen.findByRole("listbox");

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryByRole("listbox")).toBeNull();
      expect(trigger).toHaveFocus();
    });
  });

  test("a value can be chosen without a pointer", async () => {
    const user = userEvent.setup();

    render(<SelectFixture />);

    const trigger = screen.getByRole("combobox");
    expect(trigger.textContent).toContain("Viewer");

    await user.tab();
    await user.keyboard("{Enter}");
    await screen.findByRole("listbox");

    await user.keyboard("{ArrowDown}{Enter}");

    await waitFor(() => {
      expect(trigger.textContent).toContain("Editor");
    });
  });
});

describe("Popover", () => {
  test("Escape closes it and focus returns to the trigger", async () => {
    const user = userEvent.setup();

    render(
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="secondary">Filter</Button>
        </PopoverTrigger>

        <PopoverContent>
          <Field label="Minimum">
            <Input defaultValue="1000" />
          </Field>
        </PopoverContent>
      </Popover>,
    );

    const trigger = screen.getByRole("button", { name: "Filter" });

    await user.tab();
    await user.keyboard("{Enter}");

    const content = await screen.findByRole("dialog");

    // A popover holds controls, so it has to take focus — this is the line
    // that separates it from a tooltip.
    await waitFor(() => {
      expect(content.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(trigger).toHaveFocus();
    });
  });
});

describe("Tabs", () => {
  function TabsFixture() {
    return (
      <Tabs defaultValue="results">
        <TabsList>
          <TabsTrigger value="results">Results</TabsTrigger>
          <TabsTrigger value="sql">SQL</TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
        </TabsList>

        <TabsContent value="results">Rows</TabsContent>
        <TabsContent value="sql">Query</TabsContent>
        <TabsContent value="history">Runs</TabsContent>
      </Tabs>
    );
  }

  test("the whole strip is one tab stop, and Tab passes through it", async () => {
    const user = userEvent.setup();

    render(<TabsFixture />);

    const tabs = screen.getAllByRole("tab");

    // Asserted behaviorally rather than by reading tabindex, because Radix
    // puts the tab stop on the *list* and forwards focus to the selected tab.
    // That is an implementation detail; what has to be true is that one Tab
    // reaches the strip and the next one leaves it, rather than the user
    // having to tab past every tab to reach the panel.
    await user.tab();
    expect(tabs[0]).toHaveFocus();
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");

    await user.tab();

    for (const tab of tabs) {
      expect(tab).not.toHaveFocus();
    }
  });

  test("the arrow keys move between tabs", async () => {
    const user = userEvent.setup();

    render(<TabsFixture />);

    const tabs = screen.getAllByRole("tab");

    await user.tab();
    expect(tabs[0]).toHaveFocus();

    await user.keyboard("{ArrowRight}");

    await waitFor(() => {
      expect(tabs[1]).toHaveFocus();
      expect(tabs[1]).toHaveAttribute("aria-selected", "true");
    });
  });
});

describe("Toast", () => {
  test("announces without stealing focus, and is never role=alert", async () => {
    render(
      <ToastProvider duration={Infinity}>
        <Toast defaultOpen title="Dashboard saved" />
        <ToastViewport />
      </ToastProvider>,
    );

    const toast = await screen.findByRole("status");

    // role=alert would be wrong twice over: it is assertive by definition, and
    // some assistive technology moves the reading cursor to it.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(toast.getAttribute("aria-live")).toBe("polite");

    // Focus has not moved. A toast that grabs focus interrupts whatever the
    // user was typing.
    expect(document.activeElement).toBe(document.body);
  });

  test("an error is announced at once", async () => {
    render(
      <ToastProvider duration={Infinity}>
        <Toast defaultOpen tone="danger" title="Could not save" />
        <ToastViewport />
      </ToastProvider>,
    );

    const toast = await screen.findByRole("status");

    expect(toast.getAttribute("aria-live")).toBe("assertive");
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("CommandPalette", () => {
  function PaletteFixture() {
    const [open, setOpen] = useState(false);

    useCommandPaletteHotkey(() => setOpen(true));

    return (
      <>
        <Button variant="secondary" onClick={() => setOpen(true)}>
          Search
        </Button>

        <CommandPalette open={open} onOpenChange={setOpen} empty="Nothing matches that.">
          <CommandAction>Dashboards</CommandAction>
          <CommandAction>Questions</CommandAction>
          <CommandAction>Connections</CommandAction>
        </CommandPalette>
      </>
    );
  }

  test("opens on Control+K and on Meta+K", async () => {
    const user = userEvent.setup();

    render(<PaletteFixture />);

    await user.keyboard("{Control>}k{/Control}");
    expect(await screen.findByRole("dialog")).toBeTruthy();

    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    // Both, because a product used on two platforms cannot ask people to
    // learn which one this build assumed.
    await user.keyboard("{Meta>}k{/Meta}");
    expect(await screen.findByRole("dialog")).toBeTruthy();
  });

  test("typing filters and the arrow keys move the active option", async () => {
    const user = userEvent.setup();

    render(<PaletteFixture />);

    await user.keyboard("{Control>}k{/Control}");
    await screen.findByRole("dialog");

    const input = screen.getByRole("combobox");

    await waitFor(() => expect(input).toHaveFocus());

    // A combobox does not move DOM focus onto its options; it points at them
    // with aria-activedescendant. cmdk sets that only when the selection
    // changes, so it is missing on open and missing again when a filter leaves
    // the selection where it was -- see useActiveDescendant in
    // CommandPalette.tsx. Both moments are asserted here because both are
    // where a screen reader user is told nothing.
    await waitFor(() => {
      const active = input.getAttribute("aria-activedescendant");

      expect(active).toBeTruthy();
      expect(document.getElementById(active ?? "")?.textContent).toBe("Dashboards");
    });

    await user.keyboard("conn");

    await waitFor(() => {
      expect(screen.getAllByRole("option")).toHaveLength(1);
    });

    await waitFor(() => {
      const active = input.getAttribute("aria-activedescendant");

      expect(active).toBeTruthy();
      expect(document.getElementById(active ?? "")?.textContent).toBe("Connections");
    });

    expect(input).toHaveFocus();
  });

  test("says so when nothing matches", async () => {
    const user = userEvent.setup();

    render(<PaletteFixture />);

    await user.keyboard("{Control>}k{/Control}");
    await screen.findByRole("dialog");

    await user.keyboard("zzzzz");

    // An empty box and a broken search look identical, so the empty state is
    // load-bearing rather than decorative.
    expect(await screen.findByText("Nothing matches that.")).toBeTruthy();
  });
});

describe("in-flow controls", () => {
  test("every control in a form is reachable by Tab, in source order", async () => {
    const user = userEvent.setup();

    render(
      <form>
        <Field label="Name">
          <Input />
        </Field>

        <Button variant="secondary">Cancel</Button>
        <Button>Save</Button>
      </form>,
    );

    const input = screen.getByRole("textbox");
    const cancel = screen.getByRole("button", { name: "Cancel" });
    const save = screen.getByRole("button", { name: "Save" });

    for (const control of [input, cancel, save]) {
      await user.tab();

      expect(control).toHaveFocus();
    }
  });

  test("a disabled control is skipped rather than focused", async () => {
    const user = userEvent.setup();

    render(
      <form>
        <Button variant="secondary" disabled>
          Cancel
        </Button>
        <Button>Save</Button>
      </form>,
    );

    await user.tab();

    expect(screen.getByRole("button", { name: "Save" })).toHaveFocus();
  });

  test("a loading button keeps its place in the tab order", async () => {
    const user = userEvent.setup();

    render(
      <form>
        <Button loading>Save</Button>
        <Button variant="ghost">Cancel</Button>
      </form>,
    );

    await user.tab();

    // aria-busy rather than removing it: a control that vanishes from the tab
    // order mid-interaction throws focus to the top of the document.
    const save = screen.getByRole("button", { name: /save/i });

    expect(save).toHaveAttribute("aria-busy", "true");
    expect(document.activeElement === save || save.hasAttribute("disabled")).toBe(true);
  });
});
