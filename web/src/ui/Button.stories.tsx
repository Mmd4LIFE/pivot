import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";

const meta = {
  title: "Primitives/Button",
  component: Button,
  args: { children: "Save changes" },
} satisfies Meta<typeof Button>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Primary: Story = { args: { variant: "primary" } };
export const Secondary: Story = { args: { variant: "secondary" } };
export const Ghost: Story = { args: { variant: "ghost" } };

export const Danger: Story = {
  args: { variant: "danger", children: "Delete dashboard" },
};

export const Sizes: Story = {
  render: (args) => (
    <div className="flex items-center gap-3">
      <Button {...args} size="sm">
        Small
      </Button>
      <Button {...args} size="md">
        Medium
      </Button>
      <Button {...args} size="lg">
        Large
      </Button>
    </div>
  ),
};

export const Disabled: Story = { args: { disabled: true } };

/**
 * Loading keeps the control in the tab order and sets aria-busy, rather than
 * removing it — a control that vanishes from the tab order under a keyboard
 * user's fingers moves focus somewhere they did not ask for.
 */
export const Loading: Story = { args: { loading: true, children: "Saving" } };

/**
 * A control that navigates should be a link. asChild keeps the styling and
 * gives back middle-click, the context menu and the status bar preview.
 */
export const AsLink: Story = {
  args: {
    asChild: true,
    children: <a href="#continue">Continue with Okta</a>,
  },
};
