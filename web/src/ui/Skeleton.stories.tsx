import type { Meta, StoryObj } from "@storybook/react-vite";
import { Skeleton } from "./Skeleton";

const meta = {
  title: "Primitives/Skeleton",
  component: Skeleton,
  args: { className: "h-4 w-48" },
} satisfies Meta<typeof Skeleton>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Line: Story = {};

/**
 * A loading card. Every placeholder is hidden from assistive technology; the
 * status message beside them is what a screen reader hears, once.
 */
export const Card: Story = {
  render: () => (
    <div className="w-80 rounded-token border border-line bg-surface p-4">
      <p role="status" className="sr-only">
        Loading dashboards
      </p>

      <div className="flex items-center gap-3">
        <Skeleton className="size-10 rounded-full" />

        <div className="flex-1 space-y-2">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-3 w-20" />
        </div>
      </div>

      <div className="mt-4 space-y-2">
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-5/6" />
      </div>
    </div>
  ),
};
