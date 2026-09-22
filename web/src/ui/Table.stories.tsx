import type { Meta, StoryObj } from "@storybook/react-vite";
import { Badge } from "./Badge";
import { TBody, THead, Table, Td, Th, Tr } from "./Table";

// Annotated rather than `satisfies`, for the same reason as Card: the rows are
// built in each story's `render`, so `children` is never an arg.
const meta: Meta<typeof Table> = {
  title: "Data/Table",
  component: Table,
  args: { caption: "Data sources and their last sync" },
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof Table>;

const ROWS = [
  {
    name: "Warehouse",
    kind: "PostgreSQL",
    status: "Connected",
    synced: "4 minutes ago",
  },
  {
    name: "Events",
    kind: "ClickHouse",
    status: "Connected",
    synced: "12 minutes ago",
  },
  { name: "Legacy CRM", kind: "MySQL", status: "Failed", synced: "2 days ago" },
];

/** The rows, shared by the stories that differ only in the table's props. */
const sources: Story["render"] = (args) => (
  <Table {...args}>
    <THead>
      <Tr>
        <Th>Name</Th>
        <Th>Type</Th>
        <Th>Status</Th>
        <Th>Last sync</Th>
      </Tr>
    </THead>

    <TBody>
      {ROWS.map((row) => (
        <Tr key={row.name}>
          <Th scope="row" className="text-content">
            {row.name}
          </Th>
          <Td>{row.kind}</Td>
          <Td>
            <Badge tone={row.status === "Connected" ? "success" : "danger"}>
              {row.status}
            </Badge>
          </Td>
          <Td>{row.synced}</Td>
        </Tr>
      ))}
    </TBody>
  </Table>
);

export const Default: Story = { render: sources };

/** The caption still exists for a screen reader; it is only hidden visually. */
export const HiddenCaption: Story = {
  args: { captionHidden: true },
  render: sources,
};

/**
 * Sortable columns. aria-sort is set on every sortable column, including the
 * ones not currently sorted — "none" is what tells a screen reader the column
 * can be sorted at all.
 */
export const Sortable: Story = {
  render: (args) => (
    <Table {...args}>
      <THead>
        <Tr>
          <Th sort="ascending">Name</Th>
          <Th sort="none">Type</Th>
          <Th sort="none">Status</Th>
          <Th sort="none">Last sync</Th>
        </Tr>
      </THead>

      <TBody>
        {ROWS.map((row) => (
          <Tr key={row.name}>
            <Th scope="row" className="text-content">
              {row.name}
            </Th>
            <Td>{row.kind}</Td>
            <Td>{row.status}</Td>
            <Td>{row.synced}</Td>
          </Tr>
        ))}
      </TBody>
    </Table>
  ),
};
