import type { Meta, StoryObj } from "@storybook/react-vite";
import { Field } from "./Field";
import { Input } from "./Input";

const meta = {
  title: "Forms/Input",
  component: Input,
  // Every story is wrapped in a Field, because an Input without one has no
  // label — and an unlabeled input is not a thing this design system can
  // render. The decorator makes that structural rather than advisory.
  decorators: [
    (Story) => (
      <Field label="Email address">
        <Story />
      </Field>
    ),
  ],
  parameters: { layout: "padded" },
} satisfies Meta<typeof Input>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = { args: { placeholder: "you@example.com" } };

export const Filled: Story = { args: { defaultValue: "ada@example.com" } };

export const Disabled: Story = {
  args: { disabled: true, defaultValue: "ada@example.com" },
};

export const ReadOnly: Story = {
  args: { readOnly: true, defaultValue: "ada@example.com" },
};

export const Password: Story = {
  args: { type: "password", defaultValue: "correct horse battery staple" },
};
