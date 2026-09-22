import type { Meta, StoryObj } from "@storybook/react-vite";
import { Field } from "./Field";
import { Textarea } from "./Textarea";

const meta = {
  title: "Forms/Textarea",
  component: Textarea,
  decorators: [
    (Story) => (
      <Field label="SQL">
        <Story />
      </Field>
    ),
  ],
  parameters: { layout: "padded" },
} satisfies Meta<typeof Textarea>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = { args: { placeholder: "select 1" } };

export const Filled: Story = {
  args: { rows: 6, defaultValue: "select count(*)\nfrom orders\nwhere status = 'paid'" },
};

export const Disabled: Story = { args: { disabled: true, defaultValue: "select 1" } };
