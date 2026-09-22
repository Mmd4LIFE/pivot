import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import { Toast, ToastProvider, ToastViewport } from "./Toast";

const meta = {
  title: "Feedback/Toast",
  component: Toast,
  args: { title: "Dashboard saved" },
  decorators: [
    (Story) => (
      // duration Infinity so a story does not vanish while it is being read,
      // or scanned.
      <ToastProvider duration={Infinity}>
        <Story />
        <ToastViewport />
      </ToastProvider>
    ),
  ],
  parameters: { layout: "padded" },
} satisfies Meta<typeof Toast>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Neutral: Story = { args: { defaultOpen: true } };

export const Success: Story = {
  args: { defaultOpen: true, tone: "success", title: "Connection saved" },
};

export const Warning: Story = {
  args: {
    defaultOpen: true,
    tone: "warning",
    title: "Query took longer than usual",
    description: "14 seconds, against a 5 second target.",
  },
};

/**
 * The only tone that interrupts. Radix keeps `role="status"` either way and
 * raises `aria-live` to assertive, so nothing steals focus.
 */
export const Danger: Story = {
  args: {
    defaultOpen: true,
    tone: "danger",
    title: "Could not save",
    description: "The dashboard was changed by someone else while you were editing.",
  },
};

export const WithAnAction: Story = {
  args: {
    defaultOpen: true,
    title: "Dashboard deleted",
    action: (
      <Button variant="secondary" size="sm">
        Undo
      </Button>
    ),
  },
};
