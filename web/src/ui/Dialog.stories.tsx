import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import { DialogClose, DialogContent, DialogRoot, DialogTrigger } from "./Dialog";
import { Field } from "./Field";
import { Input } from "./Input";

// Annotated rather than `satisfies`: a dialog's content is structural, built in
// each story's render rather than passed as an arg.
const meta: Meta<typeof DialogContent> = {
  title: "Overlays/Dialog",
  component: DialogContent,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof DialogContent>;

/**
 * Open from the first frame, which is how the accessibility suite sees the
 * content at all — a closed dialog renders nothing to scan.
 */
export const Open: Story = {
  render: () => (
    <DialogRoot defaultOpen>
      <DialogTrigger asChild>
        <Button variant="secondary">Rename dashboard</Button>
      </DialogTrigger>

      <DialogContent
        title="Rename dashboard"
        description="Everyone with access sees the new name immediately."
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost">Cancel</Button>
            </DialogClose>
            <DialogClose asChild>
              <Button>Save</Button>
            </DialogClose>
          </>
        }
      >
        <Field label="Name">
          <Input defaultValue="Weekly revenue" />
        </Field>
      </DialogContent>
    </DialogRoot>
  ),
};

/** Closed, so the trigger is what gets scanned. */
export const Closed: Story = {
  render: () => (
    <DialogRoot>
      <DialogTrigger asChild>
        <Button variant="secondary">Rename dashboard</Button>
      </DialogTrigger>

      <DialogContent title="Rename dashboard">Nothing to see yet.</DialogContent>
    </DialogRoot>
  ),
};

export const Destructive: Story = {
  render: () => (
    <DialogRoot defaultOpen>
      <DialogTrigger asChild>
        <Button variant="danger">Delete</Button>
      </DialogTrigger>

      <DialogContent
        title="Delete this dashboard?"
        description="This cannot be undone."
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost">Cancel</Button>
            </DialogClose>
            <Button variant="danger">Delete</Button>
          </>
        }
      >
        Seven people have viewed it in the last week. They will lose access
        immediately.
      </DialogContent>
    </DialogRoot>
  ),
};

/**
 * A title that is announced but not drawn. Still required — hiding it is a
 * layout decision, leaving it out is an accessibility defect.
 */
export const HiddenTitle: Story = {
  render: () => (
    <DialogRoot defaultOpen>
      <DialogTrigger asChild>
        <Button variant="ghost">Open preview</Button>
      </DialogTrigger>

      <DialogContent title="Chart preview" titleHidden>
        A wide chart, with nothing above it competing for the space.
      </DialogContent>
    </DialogRoot>
  ),
};
