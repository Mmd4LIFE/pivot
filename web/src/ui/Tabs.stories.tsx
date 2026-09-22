import type { Meta, StoryObj } from "@storybook/react-vite";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./Tabs";

const meta: Meta<typeof Tabs> = {
  title: "Layout/Tabs",
  component: Tabs,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof Tabs>;

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="results" className="max-w-lg">
      <TabsList>
        <TabsTrigger value="results">Results</TabsTrigger>
        <TabsTrigger value="sql">SQL</TabsTrigger>
        <TabsTrigger value="history">History</TabsTrigger>
      </TabsList>

      <TabsContent value="results">1,284 rows in 240 ms.</TabsContent>
      <TabsContent value="sql">select count(*) from orders</TabsContent>
      <TabsContent value="history">Last run 4 minutes ago by Ada Lovelace.</TabsContent>
    </Tabs>
  ),
};

export const WithDisabledTab: Story = {
  render: () => (
    <Tabs defaultValue="results" className="max-w-lg">
      <TabsList>
        <TabsTrigger value="results">Results</TabsTrigger>
        <TabsTrigger value="sql">SQL</TabsTrigger>
        <TabsTrigger value="chart" disabled>
          Chart (needs a numeric column)
        </TabsTrigger>
      </TabsList>

      <TabsContent value="results">1,284 rows in 240 ms.</TabsContent>
      <TabsContent value="sql">select count(*) from orders</TabsContent>
      <TabsContent value="chart">Nothing to plot.</TabsContent>
    </Tabs>
  ),
};

/**
 * A panel that stays mounted. Its scroll position and any half-typed input
 * survive a trip to another tab, which for a query editor is the difference
 * between a tab strip and a trap.
 */
export const KeepsItsPanelMounted: Story = {
  render: () => (
    <Tabs defaultValue="results" className="max-w-lg">
      <TabsList>
        <TabsTrigger value="results">Results</TabsTrigger>
        <TabsTrigger value="sql">SQL</TabsTrigger>
      </TabsList>

      <TabsContent value="results">1,284 rows in 240 ms.</TabsContent>
      <TabsContent value="sql" forceMount className="data-[state=inactive]:hidden">
        select count(*) from orders
      </TabsContent>
    </Tabs>
  ),
};
