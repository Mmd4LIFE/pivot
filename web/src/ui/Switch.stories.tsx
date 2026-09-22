import type { Meta, StoryObj } from "@storybook/react-vite";
import { Switch } from "./Switch";

const meta = {
  title: "Forms/Switch",
  component: Switch,
  args: { label: "Require single sign-on" },
} satisfies Meta<typeof Switch>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Off: Story = {};
export const On: Story = { args: { defaultChecked: true } };
export const Disabled: Story = { args: { disabled: true } };
export const DisabledOn: Story = { args: { disabled: true, defaultChecked: true } };

/**
 * Right-to-left. The thumb mirrors, because its movement is physical rather
 * than logical — "on" is the far side of the track in whichever direction the
 * script runs.
 */
export const RightToLeft: Story = {
  args: { defaultChecked: true, label: "تفعيل الدخول الموحد" },
  render: (args) => (
    <div dir="rtl">
      <Switch {...args} />
    </div>
  ),
};
