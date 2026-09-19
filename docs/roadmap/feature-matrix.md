# Feature Matrix

The complete feature inventory for Pivot, with a competitive assessment.

**Caveat on the comparisons:** these reflect our understanding of competitor products as of
**September 2026**, based on public documentation. Competitors ship constantly and some
assessments will be stale or wrong. Treat the columns as directional positioning, not as
verified fact — and verify before using any of it in sales material.

**Legend:** ● full · ◐ partial or limited · ○ none · **$** requires a paid tier

| | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|

---

## 1. Data connectivity

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Major warehouses (Snowflake, BigQuery, Databricks, Redshift) | 1 | ● | ● | ● | ● | ● |
| Relational databases | 1 | ● | ● | ● | ● | ● |
| Query engines (Trino, Athena, Dremio) | 10 | ● | ◐ | ● | ● | ◐ |
| Lakehouse direct (Iceberg, Delta) | 10 | ● | ○ | ◐ | ◐ | ◐ |
| NoSQL and search | 10 | ◐ | ◐ | ◐ | ○ | ◐ |
| File upload (CSV, Parquet, Excel) | 1 | ● | ◐ | ○ | ○ | ● |
| SaaS sources via pipelines | 6 | ● | ○ | ○ | ○ | ● |
| **Cross-database joins** | 3 | ● | ○ | ○ | ○ | ◐ |
| SSH tunnel / mTLS | 1 | ● | ● | ◐ | ◐ | ◐ |
| Schema profiling and semantic type inference | 1 | ● | ◐ | ○ | ◐ | ◐ |

Cross-database federation is a direct consequence of embedding DuckDB. Tools that push all
computation to the source cannot do it.

## 2. Exploration

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| SQL editor with schema autocomplete | 1 | ● | ● | ● | ● | ◐ |
| Visual query builder | 2 | ● | ● | ◐ | ● | ● |
| Multi-step visual pipeline (notebook) | 2 | ● | ● | ○ | ○ | ● |
| Custom expression language | 2 | ● | ● | ◐ | ● | ● |
| Drill-down and drill-through | 2 | ● | ◐ | ◐ | ● | ● |
| Query parameters | 1 | ● | ● | ● | ● | ● |
| Query history and cost visibility | 1 | ● | ◐ | ◐ | ◐ | ○ |
| Query builds on another saved query | 2 | ● | ● | ◐ | ● | ● |

## 3. Semantic layer

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Reusable models | 3 | ● | ◐ | ◐ | ● | ● |
| **Governed metric definitions** | 3 | ● | ◐ | ◐ | ● | ● |
| Declared join graph | 3 | ● | ○ | ○ | ● | ● |
| **Fan-out-safe aggregation** | 3 | ● | ○ | ○ | ● | ◐ |
| Time intelligence (PoP, rolling, YTD) | 3 | ● | ◐ | ○ | ● | ● |
| **Git version control of definitions** | 3 | ● | ○ | ○ | ● | ◐ |
| **dbt native import and sync** | 3 | ● | ◐ | ○ | ◐ | ○ |
| Visual model authoring | 3 | ● | ◐ | ○ | ○ | ● |
| Dev/staging/prod environments | 3 | ● | ○ | ○ | ● | ◐ |
| Semantic layer exposed to external tools | 8 | ● | ○ | ○ | ● $ | ● |

This section is the core of Pivot's differentiation against the open-source field, and the
core of its differentiation against Looker is that it's YAML in Git rather than a
proprietary language requiring a specialist.

## 4. Visualization

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Chart type count | 2 | 40+ | ~20 | 50+ | ~30 | 40+ |
| **Real pivot table** (nested groups, subtotals) | 2 | ● | ◐ | ◐ | ● | ● |
| Conditional formatting | 2 | ● | ◐ | ◐ | ● | ● |
| Geospatial (choropleth, point, hexbin) | 2 | ● | ◐ | ● | ◐ | ● |
| Statistical overlays (trend, regression) | 2 | ● | ◐ | ● | ◐ | ● |
| Custom visualization plugins | 8 | ● | ◐ | ● | ◐ | ● |
| Smart default chart selection | 2 | ● | ◐ | ○ | ○ | ◐ |
| **Chart accessibility (keyboard, screen reader)** | 2 | ● | ◐ | ◐ | ◐ | ◐ |

Accessibility is marked as a differentiator deliberately. Most BI tools treat it as a
compliance checkbox; we build it into the chart abstraction so it can't be skipped.

## 5. Dashboards

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Drag-and-drop layout | 2 | ● | ● | ● | ● | ● |
| Tabs | 2 | ● | ● | ◐ | ○ | ● |
| Cross-filtering | 2 | ● | ◐ | ● | ● | ● |
| Cascading filters | 2 | ● | ◐ | ● | ● | ● |
| Time-period comparison | 2 | ● | ◐ | ◐ | ● | ● |
| Mobile-specific layout | 2 | ● | ◐ | ○ | ◐ | ● |
| **Shared-subquery consolidation** | 2 | ● | ○ | ○ | ○ | ◐ |
| Version history | 2 | ● | ◐ | ○ | ● | ◐ |
| Per-card cache policy | 2 | ● | ◐ | ◐ | ● | ◐ |

## 6. Governance & security

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| **Row-level security** | 4 | ● | ● $ | ◐ | ● | ● |
| **Column masking** | 4 | ● | ● $ | ○ | ● | ● |
| **Audit logging** | 4 | ● | ● $ | ◐ | ● | ● $ |
| **Column-level lineage** | 4 | ● | ○ | ○ | ◐ | ◐ |
| Impact analysis | 4 | ● | ○ | ○ | ● | ◐ |
| Verified / certified content | 4 | ● | ● $ | ○ | ● | ● |
| Permission debugger | 4 | ● | ○ | ○ | ◐ | ○ |
| **Query cost attribution** | 4 | ● | ○ | ○ | ◐ | ○ |
| Query budgets and throttling | 4 | ● | ○ | ◐ | ◐ | ◐ |
| Content lifecycle management | 4 | ● | ◐ | ○ | ◐ | ◐ |

**RLS, column masking, and audit logging are in Pivot's open-source core.** In Metabase and
Power BI they are paid-tier features. This is the most concrete expression of the
open-core boundary described in
[Phase 10](phase-10-ecosystem.md#6-open-source-strategy).

## 7. Alerting & distribution

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Threshold alerts | 5 | ● | ● | ● | ● | ● |
| **Anomaly detection alerts** | 5 | ● | ○ | ○ | ◐ | ◐ |
| Freshness and schema-change alerts | 5 | ● | ○ | ○ | ◐ | ○ |
| Alert hysteresis and cooldown | 5 | ● | ○ | ○ | ◐ | ◐ |
| Escalation policies | 5 | ● | ○ | ○ | ○ | ○ |
| Dashboard subscriptions | 5 | ● | ● | ● | ● | ● |
| **Per-recipient personalized reports** | 5 | ● | ◐ | ○ | ● | ● |
| PDF and Excel report generation | 5 | ● | ● | ◐ | ● | ● |
| Slack / Teams native apps | 5, 8 | ● | ● | ◐ | ● | ● |
| Conditional delivery | 5 | ● | ◐ | ○ | ◐ | ◐ |

## 8. Data pipelines

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Visual pipeline builder | 6 | ● | ○ | ○ | ○ | ● |
| SQL transforms | 6 | ● | ◐ | ○ | ○ | ● |
| **Python transforms (sandboxed)** | 6 | ● | ○ | ○ | ○ | ◐ |
| Incremental processing | 6 | ● | ○ | ○ | ○ | ◐ |
| Data quality tests | 6 | ● | ○ | ○ | ◐ | ◐ |
| Materialization and acceleration | 3, 6 | ● | ◐ $ | ○ | ● | ● |
| **Reverse ETL** | 6 | ● | ○ | ○ | ◐ | ○ |
| Live per-node data preview | 6 | ● | ○ | ○ | ○ | ● |
| Backfill and resume-from-failure | 6 | ● | ○ | ○ | ○ | ◐ |

Power BI's dataflows are the closest comparison. The open-source field has nothing here —
users run Airflow or dbt separately, with no shared lineage or permissions.

## 9. AI

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Natural-language questions | 7 | ● | ◐ $ | ○ | ● $ | ● $ |
| **Grounded in a semantic layer** | 7 | ● | ○ | ○ | ● | ◐ |
| **Shows generated SQL and assumptions** | 7 | ● | ◐ | ○ | ◐ | ◐ |
| **Refuses rather than guessing** | 7 | ● | ○ | ○ | ◐ | ○ |
| **Agentic root-cause analysis** | 7 | ● | ○ | ○ | ○ | ◐ |
| Automated insight narratives | 7 | ● | ○ | ○ | ◐ | ● |
| AI-enriched alert context | 7 | ● | ○ | ○ | ○ | ○ |
| SQL assistance and explanation | 1, 7 | ● | ◐ $ | ○ | ◐ | ◐ |
| Auto-generated documentation | 7 | ● | ◐ | ○ | ◐ | ◐ |
| **Local model / air-gapped AI** | 7 | ● | ○ | ○ | ○ | ○ |
| **Published accuracy benchmark** | 7 | ● | ○ | ○ | ○ | ○ |
| Per-org AI cost controls | 7 | ● | ○ | ○ | ◐ | ◐ |

The last two rows matter more than they appear to. Nobody publishes accuracy numbers for
their AI-BI feature, which tells you something. And no major BI vendor supports fully local
AI — which excludes every organization that can't send data to a third party.

## 10. Embedding & extensibility

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| Signed-JWT embedding with RLS | 8 | ● | ● $ | ◐ | ● $ | ● $ |
| Interactive embedding | 8 | ● | ● $ | ◐ | ● $ | ● $ |
| **Embedded self-service authoring** | 8 | ● | ◐ $ | ○ | ◐ $ | ◐ $ |
| React SDK | 8 | ● | ● $ | ○ | ◐ | ◐ |
| White-labeling | 8 | ● $ | ● $ | ◐ | ● $ | ● $ |
| Plugin system | 8 | ● | ◐ | ● | ○ | ● |
| Complete public REST API | 8 | ● | ● | ● | ● | ● |
| **Arrow Flight SQL endpoint** | 8 | ● | ○ | ○ | ○ | ○ |
| Terraform provider | 8 | ● | ◐ | ○ | ◐ | ◐ |
| Content-as-code (export/import) | 8 | ● | ◐ | ◐ | ● | ◐ |
| MCP server for AI agents | 8 | ● | ◐ | ○ | ○ | ○ |

## 11. Deployment & operations

| Feature | Phase | Pivot | Metabase | Superset | Looker | Power BI |
|---|---|---|---|---|---|---|
| **Self-hostable** | 0 | ● | ● | ● | ◐ | ○ |
| **Single-binary install** | 0 | ● | ● | ○ | ○ | ○ |
| Docker / Kubernetes | 0 | ● | ● | ● | ◐ | ○ |
| **Air-gapped deployment** | 9 | ● | ◐ | ● | ○ | ○ |
| Zero-downtime upgrades | 9 | ● | ◐ | ◐ | ● | ● |
| SSO (OIDC, SAML) | 0 | ● | ● $ | ● | ● | ● |
| SCIM provisioning | 9 | ● $ | ● $ | ○ | ● | ● |
| Multi-tenancy | 9 | ● | ◐ $ | ◐ | ◐ | ● |
| **OpenTelemetry observability** | 0 | ● | ◐ | ◐ | ○ | ○ |
| SOC 2 Type II | 9 | ● | ● | n/a | ● | ● |
| **No per-seat pricing for viewers** | — | ● | ○ | ● | ○ | ○ |

---

## Where Pivot is genuinely ahead

Distilled from the tables above. These are the claims worth making.

1. **Semantic layer + self-hosted + open source.** Looker has the layer but is neither.
   Metabase and Superset are both but lack the layer. This combination is the product.
2. **AI grounded in governed metrics, with published accuracy and local model support.**
3. **Cross-database federation and local acceleration.** A direct architectural consequence
   of embedding DuckDB.
4. **Query cost governance.** Nobody in the open-source field tells you which dashboard is
   burning the warehouse budget.
5. **Governance in the free tier.** RLS, audit, and lineage are paid upgrades elsewhere.
6. **Pipelines and BI sharing one lineage graph and one permission model.**
7. **Anomaly alerting with fatigue controls**, rather than thresholds someone has to guess.

## Where competitors are ahead, honestly

1. **Tableau and Power BI on visual authoring depth.** Years of pixel-level control we will
   not match, and shouldn't try to.
2. **Power BI on Microsoft ecosystem integration.** Excel, Teams, Office, and Fabric
   integration is structurally unmatchable.
3. **Metabase on time-to-value simplicity.** More features means more surface. Our 30-second
   commitment is a defense against this, not a victory over it.
4. **Superset on chart type count today**, though the gap is narrow and closing.
5. **Looker on semantic layer maturity.** LookML has a decade of production hardening.
   Ours will have months.
6. **Everyone on ecosystem, partners, and trained practitioners.** There is no Pivot
   consultancy. This takes years, not features.

## Where we choose not to compete

Per [the vision](../vision.md#9-what-we-are-deliberately-not-building): data warehousing,
notebooks, enterprise-wide catalogs, CDP, ML training, and desktop authoring. Each is a
real product category and none of them is ours.
