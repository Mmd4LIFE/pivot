import type { Meta, StoryObj } from "@storybook/react-vite";
import { Field } from "./Field";
import { Input } from "./Input";
import { Textarea } from "./Textarea";

const meta = {
  title: "Forms/Field",
  component: Field,
  args: {
    label: "Workspace name",
    children: <Input placeholder="Acme Analytics" />,
  },
  parameters: { layout: "padded" },
} satisfies Meta<typeof Field>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const WithDescription: Story = {
  args: {
    description: "Shown in the sidebar and in shared links.",
  },
};

export const Required: Story = { args: { required: true } };

/**
 * An error both describes the control and marks it invalid. There is no
 * separate `invalid` prop, so the two cannot drift apart.
 */
export const WithError: Story = {
  args: {
    error: "A workspace with that name already exists.",
    children: <Input defaultValue="Acme Analytics" />,
  },
};

export const WithEverything: Story = {
  args: {
    required: true,
    description: "Shown in the sidebar and in shared links.",
    error: "A workspace with that name already exists.",
    children: <Input defaultValue="Acme Analytics" />,
  },
};

export const WithTextarea: Story = {
  args: {
    label: "Description",
    description: "Markdown is not supported yet.",
    children: <Textarea placeholder="What this dashboard is for" rows={4} />,
  },
};

export const Disabled: Story = {
  args: { children: <Input disabled defaultValue="Acme Analytics" /> },
};
