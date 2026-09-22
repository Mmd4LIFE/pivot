import type { Meta, StoryObj } from "@storybook/react-vite";
import { Avatar } from "./Avatar";

const meta = {
  title: "Primitives/Avatar",
  component: Avatar,
  args: { name: "Ada Lovelace" },
} satisfies Meta<typeof Avatar>;

export default meta;

type Story = StoryObj<typeof meta>;

/** No image: initials, with the full name available to a screen reader. */
export const Initials: Story = {};

export const SingleName: Story = { args: { name: "Pivot" } };

export const Large: Story = { args: { className: "size-12 text-base" } };

/**
 * A src that cannot load. Radix falls back to the initials rather than showing
 * a broken image, which is what the delay in the component is for.
 */
export const BrokenImage: Story = {
  args: { src: "/nonexistent-avatar.png" },
};

export const Stack: Story = {
  render: () => (
    <div className="flex -space-x-2 rtl:space-x-reverse">
      <Avatar name="Ada Lovelace" />
      <Avatar name="Grace Hopper" />
      <Avatar name="Alan Turing" />
    </div>
  ),
};
