import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import { Tooltip, TooltipProvider } from "./Tooltip";

const meta = {
  title: "Overlays/Tooltip",
  component: Tooltip,
  args: { content: "Runs the query again against the source" },
  decorators: [
    (Story) => (
      <TooltipProvider>
        <Story />
      </TooltipProvider>
    ),
  ],
  parameters: { layout: "padded" },
} satisfies Meta<typeof Tooltip>;

export default meta;

type Story = StoryObj<typeof meta>;

/** Closed. A tooltip spends most of its life here and must cost nothing. */
export const Closed: Story = {
  args: { children: <Button variant="secondary">Refresh</Button> },
};

export const Open: Story = {
  args: { defaultOpen: true, children: <Button variant="secondary">Refresh</Button> },
};

/**
 * On an icon-only button. The tooltip is not the accessible name — the
 * visually hidden text is. A tooltip does not appear on a touch device, and a
 * control whose only name is a tooltip has no name there at all.
 */
export const OnAnIconButton: Story = {
  args: {
    defaultOpen: true,
    content: "Refresh",
    children: (
      <Button variant="ghost" size="sm" className="px-2">
        <svg viewBox="0 0 16 16" aria-hidden="true" className="size-4">
          <path
            d="M13 8a5 5 0 1 1-1.5-3.5M13 2v3h-3"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
        <span className="sr-only">Refresh</span>
      </Button>
    ),
  },
};

export const Sides: Story = {
  args: { children: <Button variant="secondary">Refresh</Button> },
  render: () => (
    <div className="flex gap-3">
      <Tooltip content="Above" side="top" defaultOpen>
        <Button variant="secondary" size="sm">
          Top
        </Button>
      </Tooltip>

      <Tooltip content="Below" side="bottom" defaultOpen>
        <Button variant="secondary" size="sm">
          Bottom
        </Button>
      </Tooltip>
    </div>
  ),
};
