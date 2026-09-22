import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import { EmptyState } from "./EmptyState";

const meta = {
  title: "Feedback/EmptyState",
  component: EmptyState,
  args: { title: "No dashboards yet" },
  parameters: { layout: "padded" },
} satisfies Meta<typeof EmptyState>;

export default meta;

type Story = StoryObj<typeof meta>;

export const TitleOnly: Story = {};

export const WithDescription: Story = {
  args: {
    description:
      "A dashboard collects questions onto one page. Anyone with access to the underlying data can see it.",
  },
};

export const WithAction: Story = {
  args: {
    description: "A dashboard collects questions onto one page.",
    action: <Button>Create a dashboard</Button>,
  },
};

/**
 * Empty because a filter excluded everything, which is a different message
 * from "you have none" — and the difference is what stops a user believing
 * the product lost their work.
 */
export const FilteredToNothing: Story = {
  args: {
    title: "No dashboards match those filters",
    description: "Try removing the owner filter.",
    action: <Button variant="secondary">Clear filters</Button>,
  },
};
