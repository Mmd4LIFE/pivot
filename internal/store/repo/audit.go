package repo

import (
	"context"
	"log/slog"
)

// AuditSubscriber logs every mutation.
//
// This is a stand-in for the durable audit log in Phase 4, which writes to the
// tamper-evident `audit_log` table. Structured logging now means the shape of
// an audit record — who, what, which tenant, when — is exercised from the
// start, so the Phase 4 work is a change of sink rather than a change of
// design.
//
// The subscriber never fails a write: the event bus recovers panics, and this
// handler only logs. Logging is deliberately the *only* thing it does, because
// anything slower would delay the caller — handlers run synchronously.
func AuditSubscriber(log *slog.Logger) EventHandler {
	return func(ctx context.Context, e ChangeEvent) {
		attrs := []slog.Attr{
			slog.String("kind", string(e.Kind)),
			slog.String("entity_type", e.EntityType),
			slog.String("entity_id", e.EntityID.String()),
			slog.String("org_id", e.OrgID.String()),
			slog.String("at", e.At.String()),
		}

		// A system action has no actor, and recording that honestly matters:
		// inventing a user for a migration or a scheduled job would make the
		// audit trail lie.
		if e.ActorID.Valid {
			attrs = append(attrs, slog.String("actor_id", e.ActorID.UUID.String()))
		} else {
			attrs = append(attrs, slog.String("actor_id", "system"))
		}

		log.LogAttrs(ctx, slog.LevelInfo, "audit", attrs...)
	}
}
