import type { Meta, StoryObj } from "@storybook/react-vite";
import { Alert } from "./Alert";

const meta = {
  title: "Feedback/Alert",
  component: Alert,
  args: { children: "Your last sync finished 4 minutes ago." },
  parameters: { layout: "padded" },
} satisfies Meta<typeof Alert>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Info: Story = { args: { tone: "info" } };

export const Success: Story = {
  args: { tone: "success", children: "The connection was saved." },
};

export const Warning: Story = {
  args: {
    tone: "warning",
    children: "This query has not run in 30 days and may be out of date.",
  },
};

export const Danger: Story = {
  args: {
    tone: "danger",
    title: "Could not reach the database",
    children: "Check the host and port, then try again.",
  },
};

/**
 * Announced when it appears. Only for a message that follows something the
 * user did — a live region on a banner that was there from the first paint
 * interrupts the page's own heading.
 */
export const Announced: Story = {
  args: {
    tone: "danger",
    live: true,
    title: "Save failed",
    children: "The dashboard was changed by someone else while you were editing.",
  },
};

export const AllTones: Story = {
  render: () => (
    <div className="flex max-w-xl flex-col gap-3">
      <Alert tone="info">A scheduled refresh is queued.</Alert>
      <Alert tone="success">The connection was saved.</Alert>
      <Alert tone="warning">This query has not run in 30 days.</Alert>
      <Alert tone="danger">Could not reach the database.</Alert>
    </div>
  ),
};
