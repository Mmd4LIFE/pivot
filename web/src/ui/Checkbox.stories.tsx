import type { Meta, StoryObj } from "@storybook/react-vite";
import { Checkbox } from "./Checkbox";

const meta = {
  title: "Forms/Checkbox",
  component: Checkbox,
  args: { label: "Send me a weekly digest" },
} satisfies Meta<typeof Checkbox>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Unchecked: Story = {};
export const Checked: Story = { args: { defaultChecked: true } };

/** The tristate a "select all" header needs when only some rows are selected. */
export const Indeterminate: Story = {
  args: { checked: "indeterminate", label: "Select all rows" },
};

export const Disabled: Story = { args: { disabled: true } };

export const DisabledChecked: Story = {
  args: { disabled: true, defaultChecked: true },
};

export const Group: Story = {
  render: () => (
    <fieldset className="flex flex-col gap-3 border-0 p-0">
      <legend className="mb-2 text-sm font-medium">Email me about</legend>
      <Checkbox label="Failed scheduled queries" defaultChecked />
      <Checkbox label="New members joining" />
      <Checkbox label="Weekly usage summary" />
    </fieldset>
  ),
};
