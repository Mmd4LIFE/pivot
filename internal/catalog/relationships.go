package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
Relationships, through the same reconcile-and-sweep as everything else.

They are stored a column at a time, so a two-column key is two rows sharing a
constraint name and ordered by [connectors.ForeignKey.Ordinal]. That is what
the source gives and what a relational store can hold; reassembling them is
[connectors.GroupForeignKeys], and Phase 3's join inference is the caller.

The reported change is per *relationship*, not per column. "added
public.orders (orders_customer_fkey)" is what somebody needs; two lines saying
a column of it appeared is noise that buries the one thing worth reading.
*/

// keyIdentity is what makes one column of one relationship unique.
//
// The table is part of it as well as the constraint name, because a name is
// unique per table in MySQL and per schema elsewhere -- keying on the name
// alone merges two tables' constraints into one wrong relationship.
type keyIdentity struct {
	schema     string
	table      string
	constraint string
	ordinal    int64
}

// relationIdentity names a whole relationship, for reporting.
type relationIdentity struct {
	schema     string
	table      string
	constraint string
}

/*
reconcileRelationships records what the source declares and reports what moved.

A source that cannot report relationships at all is not an error and does not
sweep. [connectors.ErrNoForeignKeys] means nobody looked, and marking every
stored relationship gone on the strength of not having asked would delete a
schema's structure because a connector lacks a feature.
*/
func (s *Syncer) reconcileRelationships(
	ctx context.Context,
	connectionID uuid.UUID,
	source connectors.Connector,
	report *Report,
	at time.Time,
) error {
	keys, err := source.ForeignKeys(ctx)

	if errors.Is(err, connectors.ErrNoForeignKeys) {
		report.ForeignKeysUnavailable = true

		return nil
	}

	if err != nil {
		return fmt.Errorf("read the source's foreign keys: %w", err)
	}

	stored, err := s.store.ForeignKeys(ctx, connectionID)
	if err != nil {
		return fmt.Errorf("read the stored foreign keys: %w", err)
	}

	before := indexForeignKeys(stored)
	seen := map[keyIdentity]bool{}

	// Which relationships were new, tracked per relationship rather than per
	// column so that a composite key is reported once.
	appeared := map[relationIdentity]bool{}
	present := map[relationIdentity]bool{}

	for _, key := range keys {
		identity := keyIdentity{
			schema: key.FromSchema, table: key.FromTable,
			constraint: key.Name, ordinal: int64(key.Ordinal),
		}
		seen[identity] = true

		relation := relationIdentity{
			schema: key.FromSchema, table: key.FromTable, constraint: key.Name,
		}
		present[relation] = true

		was, known := before[identity]
		if !known || was.RemovedAt.Valid {
			appeared[relation] = true
		}

		if _, rerr := s.store.RecordForeignKey(ctx, seenKey(connectionID, key), at); rerr != nil {
			return fmt.Errorf("record %s on %s.%s: %w",
				key.Name, key.FromSchema, key.FromTable, rerr)
		}
	}

	report.RelationshipsSeen = len(present)

	gone, err := s.store.MarkForeignKeysGone(ctx, connectionID, at)
	if err != nil {
		return fmt.Errorf("mark relationships gone: %w", err)
	}

	removed := missingRelationships(before, seen)

	// The same cross-check the table sweep makes: the database counts rows and
	// this names relationships, and a disagreement is the signature of a
	// second sync running against the same connection.
	if named := countColumnsOf(before, removed); int64(named) != gone {
		report.Changes = append(report.Changes, Change{
			Kind: Changed,
			Detail: fmt.Sprintf(
				"the sweep marked %d foreign key columns gone and %d were named; "+
					"another sync may be running against this connection", gone, named),
		})
	}

	report.Changes = append(report.Changes, relationChanges(appeared, removed)...)

	return nil
}

func seenKey(connectionID uuid.UUID, key connectors.ForeignKey) repo.SeenForeignKey {
	return repo.SeenForeignKey{
		ConnectionID: connectionID,
		Constraint:   key.Name,
		FromSchema:   key.FromSchema,
		FromTable:    key.FromTable,
		FromColumn:   key.FromColumn,
		ToSchema:     key.ToSchema,
		ToTable:      key.ToTable,
		ToColumn:     key.ToColumn,
		Ordinal:      int64(key.Ordinal),
	}
}

func indexForeignKeys(stored []model.CatalogForeignKey) map[keyIdentity]model.CatalogForeignKey {
	out := make(map[keyIdentity]model.CatalogForeignKey, len(stored))

	for _, key := range stored {
		out[keyIdentity{
			schema: key.FromSchema, table: key.FromTable,
			constraint: key.ConstraintName, ordinal: key.Ordinal,
		}] = key
	}

	return out
}

// missingRelationships lists relationships that were stored, live, and not
// seen by this sync.
func missingRelationships(
	before map[keyIdentity]model.CatalogForeignKey, seen map[keyIdentity]bool,
) map[relationIdentity]bool {
	gone := map[relationIdentity]bool{}

	for identity, key := range before {
		if seen[identity] || key.RemovedAt.Valid {
			continue
		}

		gone[relationIdentity{
			schema: identity.schema, table: identity.table, constraint: identity.constraint,
		}] = true
	}

	return gone
}

// countColumnsOf counts the stored, live columns belonging to a set of
// relationships -- which is what the sweep's row count should equal.
func countColumnsOf(
	before map[keyIdentity]model.CatalogForeignKey, relations map[relationIdentity]bool,
) int {
	n := 0

	for identity, key := range before {
		if key.RemovedAt.Valid {
			continue
		}

		if relations[relationIdentity{
			schema: identity.schema, table: identity.table, constraint: identity.constraint,
		}] {
			n++
		}
	}

	return n
}

/*
relationChanges turns the two sets into sorted, reportable changes.

A new relationship pointing at a table Pivot has not cataloged is reported with
that said. It is stored either way -- a schema granted piecemeal is the
ordinary reason, and dropping the key for tidiness would throw away the only
record that the relationship exists -- but somebody reading the report should
know the join it enables has one end missing.
*/
func relationChanges(appeared, removed map[relationIdentity]bool) []Change {
	var changes []Change

	for relation := range appeared {
		change := Change{
			Kind: Added, Schema: relation.schema, Table: relation.table,
			Constraint: relation.constraint,
		}

		changes = append(changes, change)
	}

	for relation := range removed {
		changes = append(changes, Change{
			Kind: Removed, Schema: relation.schema, Table: relation.table,
			Constraint: relation.constraint,
		})
	}

	sortChanges(changes)

	return changes
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}

		if a.Table != b.Table {
			return a.Table < b.Table
		}

		return a.Constraint < b.Constraint
	})
}
