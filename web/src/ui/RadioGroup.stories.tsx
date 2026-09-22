import type { Meta, StoryObj } from "@storybook/react-vite";
import { Radio, RadioGroup } from "./RadioGroup";

const meta = {
  title: "Forms/RadioGroup",
  component: RadioGroup,
  args: { defaultValue: "viewer", "aria-label": "Default role" },
} satisfies Meta<typeof RadioGroup>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: (args) => (
    <RadioGroup {...args}>
      <Radio value="viewer" label="Viewer" />
      <Radio value="editor" label="Editor" />
      <Radio value="admin" label="Admin" />
    </RadioGroup>
  ),
};

/**
 * The group is one tab stop and the arrow keys move between options. That is
 * the ARIA radio pattern, and it is the part hand-rolled radio groups miss.
 */
export const Labeled: Story = {
  render: (args) => (
    <fieldset className="border-0 p-0">
      <legend className="mb-2 text-sm font-medium">Default role for new members</legend>

      <RadioGroup {...args}>
        <Radio value="viewer" label="Viewer" />
        <Radio value="editor" label="Editor" />
        <Radio value="admin" label="Admin" />
      </RadioGroup>
    </fieldset>
  ),
};

export const WithDisabledOption: Story = {
  render: (args) => (
    <RadioGroup {...args}>
      <Radio value="viewer" label="Viewer" />
      <Radio value="editor" label="Editor" />
      <Radio value="owner" label="Owner (only one per organization)" disabled />
    </RadioGroup>
  ),
};
