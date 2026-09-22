import type { Meta, StoryObj } from "@storybook/react-vite";
import { Badge } from "./Badge";

const meta = {
  title: "Primitives/Badge",
  component: Badge,
  args: { children: "Active" },
} satisfies Meta<typeof Badge>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Neutral: Story = { args: { tone: "neutral", children: "Draft" } };
export const Accent: Story = { args: { tone: "accent", children: "Beta" } };
export const Success: Story = { args: { tone: "success", children: "Connected" } };
export const Warning: Story = { args: { tone: "warning", children: "Degraded" } };
export const Danger: Story = { args: { tone: "danger", children: "Failed" } };
export const Info: Story = { args: { tone: "info", children: "Scheduled" } };

/**
 * All tones together. The text says what each one means, so the badges are
 * still readable with the color removed — which is the test that matters.
 */
export const AllTones: Story = {
  render: () => (
    <div className="flex flex-wrap gap-2">
      <Badge tone="neutral">Draft</Badge>
      <Badge tone="accent">Beta</Badge>
      <Badge tone="success">Connected</Badge>
      <Badge tone="warning">Degraded</Badge>
      <Badge tone="danger">Failed</Badge>
      <Badge tone="info">Scheduled</Badge>
    </div>
  ),
};
