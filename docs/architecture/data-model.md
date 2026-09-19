# Metadata Data Model

**Status:** Proposed · **Last reviewed:** 2026-09-19

The schema Pivot uses to describe itself. Everything else hangs off this.

Conventions: all tables carry `id` (UUID v7, time-sortable), `org_id`, `created_at`,
`updated_at`, `created_by`, `updated_by`, and `deleted_at` (soft delete) unless noted.
`version` (integer) is present on editable entities for optimistic concurrency.

**Portability constraint:** every table and query below must work on both PostgreSQL 16+
and SQLite 3.45+. JSONB on Postgres maps to JSON text on SQLite; `pgvector` columns exist
only on Postgres, and features depending on them degrade rather than break.

---

## 1. Identity & tenancy

```
organizations
  id, name, slug, settings (jsonb), plan, created_at
  ─ the tenant root. Every other table references org_id.

users
  id, org_id, email, name, avatar_url, password_hash (nullable),
  is_active, last_login_at, locale, timezone
  UNIQUE (org_id, email)

user_attributes
  id, org_id, user_id, key, value, source (manual|oidc|saml|scim)
  UNIQUE (user_id, key)
  ─ feeds RLS policies. `source` records provenance so IdP-synced
    attributes aren't silently overwritten by manual edits.

groups
  id, org_id, name, description, parent_group_id (nullable), external_id
  ─ nested groups; external_id links to the IdP for SCIM sync.

group_members
  group_id, user_id, added_at, added_by
  PRIMARY KEY (group_id, user_id)

sessions
  id, org_id, user_id, token_hash, expires_at, ip, user_agent,
  last_seen_at, revoked_at
  ─ server-side; revocation is immediate by design.

api_keys
  id, org_id, user_id (nullable for service accounts), name,
  prefix, key_hash, scopes (jsonb), expires_at, last_used_at, revoked_at
  ─ prefix is stored plaintext for display and lookup; only the hash
    can authenticate.

identity_providers
  id, org_id, type (oidc|saml), name, config (jsonb, encrypted),
  is_enabled, attribute_mapping (jsonb), group_mapping (jsonb)
```

---

## 2. Connections & catalog

```
connections
  id, org_id, name, type, config (jsonb, encrypted), status,
  last_tested_at, sync_schedule, resource_limits (jsonb)
  ─ config holds credentials, envelope-encrypted. Never returned by
    the API, never logged.

catalog_schemas
  id, org_id, connection_id, name, description

catalog_tables
  id, org_id, connection_id, schema_id, name, type (table|view|mv),
  description, row_count_estimate, bytes_estimate,
  last_synced_at, is_hidden

catalog_columns
  id, org_id, table_id, name, position, source_type, pivot_type,
  semantic_type, is_nullable, is_primary_key, description,
  profile (jsonb), pii_tags (jsonb)
  ─ pivot_type is the normalized type; semantic_type is inferred
    (email, url, currency, country, …) and drives default formatting,
    chart selection, and AI grounding.
  ─ profile holds cardinality, null rate, min/max, and value samples.

catalog_relationships
  id, org_id, from_column_id, to_column_id, cardinality,
  source (declared|foreign_key|inferred), confidence
  ─ `source` and `confidence` keep inferred joins visibly distinct
    from declared ones.
```

**`catalog_columns.profile` and `semantic_type` are load-bearing** well beyond the catalog
UI: they drive smart chart defaults ([P2-VCF-012](../roadmap/phase-2-visualization-dashboards.md)),
join suggestions, and — most importantly — AI grounding in Phase 7. Profiling early is what
makes later phases work.

---

## 3. Semantic layer

```
semantic_projects
  id, org_id, name, git_repo_url, git_branch, git_path,
  last_synced_at, sync_status
  ─ a project maps to a Git repository or a UI-managed set.

semantic_models
  id, org_id, project_id, name, label, description,
  connection_id, source_type (table|view|sql|flow_output),
  source_ref, sql (nullable), is_verified, owner_id,
  acceleration_policy (jsonb), definition (jsonb)
  UNIQUE (project_id, name)

semantic_dimensions
  id, org_id, model_id, name, label, description, type,
  expression, semantic_type, format (jsonb), hierarchy_id,
  is_hidden, is_primary_key

semantic_measures
  id, org_id, model_id, name, label, description,
  aggregation, expression, filters (jsonb), format (jsonb)
  ─ a measure is an aggregation over one model's columns.

semantic_metrics
  id, org_id, project_id, name, label, description,
  type (simple|ratio|derived|cumulative), definition (jsonb),
  format (jsonb), is_verified, verified_by, verified_at,
  owner_id, tags (jsonb)
  ─ a metric is a named business calculation, possibly spanning
    models. This is the governed object everything else references.

semantic_joins
  id, org_id, project_id, from_model_id, to_model_id,
  type, cardinality, condition (jsonb), is_fan_out_safe
  ─ the join graph. is_fan_out_safe records whether symmetric
    aggregates are required.

semantic_hierarchies
  id, org_id, model_id, name, levels (jsonb)
  ─ drill-down paths, e.g. country → state → city.

rls_policies
  id, org_id, model_id, name, type (attribute|lookup|group),
  definition (jsonb), is_enabled, priority
  ─ compiled into WHERE predicates by the semantic compiler.

column_masks
  id, org_id, model_id, column_name, type (hash|redact|partial|null),
  condition (jsonb), config (jsonb)

semantic_versions
  id, org_id, project_id, git_sha, author, message,
  snapshot (jsonb), created_at
  ─ immutable history; supports diff and rollback.
```

**Why metrics are separate from measures.** A measure (`SUM(orders.amount)`) belongs to one
model. A metric (`net_revenue_growth`) is a business concept that may combine measures
across models, apply filters, and compare periods. Conflating them is the mistake that
makes a semantic layer feel like a thin wrapper over SQL.

---

## 4. Content

```
collections
  id, org_id, name, description, parent_id, type (personal|shared|official),
  owner_id, icon, position
  ─ nested; permissions inherit down unless overridden.

questions
  id, org_id, collection_id, name, description, type (sql|builder|semantic),
  query (jsonb), visualization (jsonb), connection_id, model_id,
  cache_policy (jsonb), is_verified, verified_by, verified_at,
  owner_id, tags (jsonb), version
  ─ `query` holds either raw SQL, a structured builder query, or a
    semantic query, per `type`.

dashboards
  id, org_id, collection_id, name, description, layout (jsonb),
  tabs (jsonb), filters (jsonb), theme (jsonb), auto_refresh_seconds,
  cache_policy (jsonb), is_verified, owner_id, tags (jsonb), version

dashboard_cards
  id, org_id, dashboard_id, tab_id, question_id (nullable),
  type (question|text|image|iframe|divider), position (jsonb),
  visualization_override (jsonb), filter_mappings (jsonb),
  click_behavior (jsonb)
  ─ visualization_override lets one saved question render differently
    per dashboard without duplicating it.

content_versions
  id, org_id, entity_type, entity_id, version, snapshot (jsonb),
  author_id, message, created_at

comments
  id, org_id, entity_type, entity_id, parent_id, body,
  anchor (jsonb), author_id, resolved_at, resolved_by
  ─ anchor pins a comment to a data point or a chart region.

favorites / recently_viewed
  org_id, user_id, entity_type, entity_id, at
```

---

## 5. Alerts & subscriptions

```
alerts
  id, org_id, name, description, source_type (question|metric|sql),
  source_id, condition (jsonb), schedule (jsonb),
  hysteresis (jsonb), cooldown_seconds, state, last_evaluated_at,
  last_fired_at, owner_id, is_enabled, error_count

alert_recipients
  id, alert_id, type (user|group|channel|webhook), target_id,
  channel_config (jsonb)

alert_events
  id, org_id, alert_id, state_from, state_to, evaluated_value (jsonb),
  threshold (jsonb), context (jsonb), created_at
  ─ full evaluation history; `context` carries AI-generated
    explanation from Phase 7.

subscriptions
  id, org_id, name, entity_type, entity_id, schedule (jsonb),
  format (pdf|excel|inline|png), filters (jsonb),
  conditional_send (jsonb), owner_id, is_enabled

subscription_recipients
  id, subscription_id, type, target_id, channel, personalize (bool)

deliveries
  id, org_id, source_type (alert|subscription), source_id,
  recipient_id, channel, status, attempts, error, idempotency_key,
  sent_at
  ─ the transactional outbox. idempotency_key prevents duplicate
    delivery on retry.
```

---

## 6. Flows

```
flows
  id, org_id, name, description, definition (jsonb),
  schedule (jsonb), is_enabled, owner_id, timeout_seconds, version
  ─ definition is the DAG: nodes, edges, and node configs.

flow_runs
  id, org_id, flow_id, status, trigger (schedule|manual|event|upstream),
  parameters (jsonb), started_at, finished_at, error, triggered_by

flow_node_runs
  id, org_id, flow_run_id, node_id, status, started_at, finished_at,
  rows_in, rows_out, bytes_processed, checkpoint_ref, error, logs_ref
  ─ checkpoint_ref points at materialized intermediate output on
    object storage. This is what makes resume-from-failure possible.

flow_state
  id, org_id, flow_id, node_id, key, value (jsonb), updated_at
  ─ watermarks and incremental-processing state. Advances only on
    confirmed commit.

data_quality_tests
  id, org_id, target_type (model|table|flow_node), target_id,
  type, config (jsonb), severity (warn|fail), is_enabled

data_quality_results
  id, org_id, test_id, run_id, status, failed_rows, details (jsonb),
  created_at
```

---

## 7. Operations & audit

```
query_log
  id, org_id, user_id, connection_id, source_type, source_id,
  sql, semantic_query (jsonb), duration_ms, rows_returned,
  bytes_scanned, estimated_cost, cache_status, error,
  trace_id, started_at
  PARTITION BY RANGE (started_at)
  ─ powers query governance, cost attribution, and AI few-shot
    context. Partitioned and retention-managed.

audit_log
  id, org_id, actor_id, actor_type, action, entity_type, entity_id,
  changes (jsonb), ip, user_agent, trace_id, hash_prev, hash,
  created_at
  PARTITION BY RANGE (created_at)
  ─ hash/hash_prev form a tamper-evident chain.

jobs                     (managed by River)
  id, org_id, kind, args, state, attempt, max_attempts,
  scheduled_at, finished_at, errors

cache_entries
  key, org_id, tier, generation, size_bytes, model_ids (jsonb),
  policy_hash, expires_at, created_at, last_accessed_at
  ─ metadata only; the bytes live in Valkey or object storage.
  ─ generation + model_ids implement generation-based invalidation.

usage_events
  id, org_id, user_id, entity_type, entity_id, event, created_at
  PARTITION BY RANGE (created_at)
  ─ feeds content lifecycle and usage analytics.
```

---

## 8. AI

```
embeddings                                    [Postgres only]
  id, org_id, entity_type, entity_id, content, embedding vector(1536),
  model, updated_at
  ─ pgvector. On SQLite, semantic search degrades to full-text.

glossary_terms
  id, org_id, term, definition, synonyms (jsonb),
  related_metric_ids (jsonb), owner_id
  ─ the highest-leverage customer-authored input to AI accuracy.

ai_conversations
  id, org_id, user_id, title, context (jsonb), created_at

ai_messages
  id, org_id, conversation_id, role, content,
  semantic_query (jsonb), generated_sql, models_used (jsonb),
  confidence, tokens_in, tokens_out, cost, model_id,
  feedback (jsonb), created_at
  ─ the complete AI audit trail required by P7-SAF-007, and the
    feedback loop's training data.

ai_evaluations
  id, org_id, benchmark_id, question, expected (jsonb), actual (jsonb),
  score, model_id, prompt_version, created_at
```

---

## 9. Design notes

### UUID v7 for primary keys
Time-sortable, so they index well without a separate sequence, and they're safe to expose
in URLs and generate client-side. Monotonic enough to avoid the index fragmentation that
makes UUID v4 primary keys a performance problem.

### JSONB for structure that varies, columns for structure that's queried
Chart configurations, flow definitions, and filter specs are JSONB — they vary by type,
evolve constantly, and are read as a whole. Anything filtered, joined, or aggregated on
gets a real column. The rule: **if you'd want an index on it, it's a column.**

### Soft delete everywhere, with a purge job
`deleted_at` on everything, because accidental deletion of a dashboard someone depends on
is common and recovery must be trivial. A scheduled job hard-deletes after the retention
window. All queries filter `deleted_at IS NULL` at the repository layer, not per call site.

### Partitioning on the three high-volume tables
`query_log`, `audit_log`, and `usage_events` are the only tables that grow without bound —
at scale they dominate everything else combined. Monthly range partitions with automated
creation and retention-based drop. On SQLite, partitioning is absent and retention is
enforced by delete.

### Everything references `org_id`, including child tables
Denormalized deliberately. A child table could derive its org from its parent, but carrying
`org_id` directly means the repository layer can scope *every* query uniformly and a missing
join can never widen the scope. The redundancy buys a structural guarantee.

---

## 10. Entity relationships

```
organizations ─┬─ users ─── user_attributes
               ├─ groups ── group_members
               ├─ connections ─── catalog_schemas ─── catalog_tables ─── catalog_columns
               │                                                             │
               ├─ semantic_projects ─┬─ semantic_models ──────────────────────┘
               │                     │     ├─ semantic_dimensions
               │                     │     ├─ semantic_measures
               │                     │     ├─ rls_policies
               │                     │     └─ column_masks
               │                     ├─ semantic_metrics
               │                     └─ semantic_joins
               │
               ├─ collections ─┬─ questions ──────┐
               │               └─ dashboards ─── dashboard_cards
               │
               ├─ alerts ─── alert_recipients, alert_events
               ├─ subscriptions ─── subscription_recipients ─── deliveries
               ├─ flows ─── flow_runs ─── flow_node_runs
               └─ query_log, audit_log, usage_events
```
