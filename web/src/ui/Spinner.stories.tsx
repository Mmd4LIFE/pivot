import type { Meta, StoryObj } from "@storybook/react-vite";
import { Spinner } from "./Spinner";

const meta = {
  title: "Primitives/Spinner",
  component: Spinner,
  args: { className: "size-6 text-accent" },
} satisfies Meta<typeof Spinner>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const CustomLabel: Story = { args: { label: "Running query" } };

/** Silent, for a spinner inside a control that already says what is happening. */
export const Decorative: Story = { args: { label: null } };
