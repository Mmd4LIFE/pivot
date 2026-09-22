import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { Button } from "./Button";
import { CommandAction, CommandGroupItems, CommandPalette } from "./CommandPalette";

const meta: Meta<typeof CommandPalette> = {
  title: "Overlays/CommandPalette",
  component: CommandPalette,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof CommandPalette>;

// cmdk names its group heading with a data attribute rather than a class, so
// the heading is styled through it. Repeated rather than abstracted because
// there are exactly two groups here and a helper would hide what it does.
const HEADING =
  "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 " +
  "[&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium " +
  "[&_[cmdk-group-heading]]:text-content-muted";

const ITEMS = (
  <>
    <CommandGroupItems heading="Go to" className={HEADING}>
      <CommandAction>Dashboards</CommandAction>
      <CommandAction>Questions</CommandAction>
      <CommandAction>Connections</CommandAction>
    </CommandGroupItems>

    <CommandGroupItems heading="Create" className={HEADING}>
      <CommandAction>New dashboard</CommandAction>
      <CommandAction>New question</CommandAction>
      <CommandAction disabled>New model (needs a connection)</CommandAction>
    </CommandGroupItems>
  </>
);

export const Open: Story = {
  render: () => (
    <CommandPalette open onOpenChange={() => {}}>
      {ITEMS}
    </CommandPalette>
  ),
};

/** Closed, which is the state the trigger has to stand on its own in. */
export const Closed: Story = {
  render: function Closed() {
    const [open, setOpen] = useState(false);

    return (
      <>
        <Button variant="secondary" onClick={() => setOpen(true)}>
          Search
        </Button>

        <CommandPalette open={open} onOpenChange={setOpen}>
          {ITEMS}
        </CommandPalette>
      </>
    );
  },
};

/**
 * A search with no matches. It says so rather than showing an empty box, which
 * is otherwise indistinguishable from a broken search.
 */
export const NoMatches: Story = {
  render: () => (
    <CommandPalette open onOpenChange={() => {}} empty="Nothing matches that.">
      {null}
    </CommandPalette>
  ),
};
