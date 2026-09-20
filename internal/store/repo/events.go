package repo

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
)

// ChangeKind describes what happened to an entity.
type ChangeKind string

const (
	ChangeCreated ChangeKind = "created"
	ChangeUpdated ChangeKind = "updated"
	ChangeDeleted ChangeKind = "deleted"
)

// ChangeEvent records a mutation. The audit log (Phase 4) and the search index
// (Phase 1) are built on this rather than on database triggers, so that the
// event carries the acting user — something the database does not know.
type ChangeEvent struct {
	Kind       ChangeKind
	EntityType string
	EntityID   uuid.UUID
	OrgID      uuid.UUID
	ActorID    uuid.NullUUID
	At         dbtypes.Time
}

// EventHandler consumes change events.
//
// Handlers run synchronously, inside the caller's goroutine but outside its
// transaction. They must be fast and must not fail the write: a subscriber
// that panics is contained, and one that errors is its own problem. Anything
// slow belongs on a queue, which arrives with River in Phase 5.
type EventHandler func(context.Context, ChangeEvent)

// EventBus fans change events out to subscribers.
type EventBus struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

// NewEventBus returns an empty bus.
func NewEventBus() *EventBus { return &EventBus{} }

// Subscribe registers a handler. Handlers cannot be removed; the bus lives as
// long as the process, and a removable subscription is a lifecycle problem
// nobody has needed yet.
func (b *EventBus) Subscribe(h EventHandler) {
	if h == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers = append(b.handlers, h)
}

// publish delivers an event to every subscriber.
//
// A panicking handler is recovered so that a faulty subscriber cannot take
// down the write that triggered it. The write has already committed by this
// point; failing here would report an error for an operation that succeeded.
func (b *EventBus) publish(ctx context.Context, e ChangeEvent) {
	b.mu.RLock()
	handlers := make([]EventHandler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if recover() != nil {
					// Deliberately swallowed. The write that produced this
					// event has already committed, so propagating a
					// subscriber's panic would report a failure for an
					// operation that succeeded. Subscribers that need to
					// report problems must do so themselves.
					_ = e
				}
			}()

			h(ctx, e)
		}()
	}
}

// emit publishes a change event for an entity.
func (b base) emit(
	ctx context.Context,
	kind ChangeKind,
	entityType string,
	entityID, orgID uuid.UUID,
	actorID uuid.NullUUID,
) {
	if b.events == nil {
		return
	}

	b.events.publish(ctx, ChangeEvent{
		Kind:       kind,
		EntityType: entityType,
		EntityID:   entityID,
		OrgID:      orgID,
		ActorID:    actorID,
		At:         dbtypes.Now(),
	})
}
