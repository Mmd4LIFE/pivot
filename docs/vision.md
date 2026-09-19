# Pivot — Product Vision

> **One sentence:** Pivot is a self-hostable business intelligence platform that combines
> a governed semantic layer, no-code and SQL exploration, world-class visualization,
> alerting, data flows, and a grounded AI analyst — in a product that is as easy to start
> as Metabase and as trustworthy as Looker.

---

## 1. The problem

Every company that reaches ~50 employees hits the same wall. They have data in a
warehouse. They have questions. And the path between the two is broken in one of four
predictable ways.

### 1.1 The "easy tool" ceiling

Teams start with Metabase or a spreadsheet export. It works beautifully for six months.
Then:

- Three dashboards define "active user" three different ways, and nobody can say which is
  right.
- The finance dashboard and the growth dashboard disagree on revenue by 4%, and resolving
  it takes two analysts a week.
- Someone's ad-hoc SQL question becomes load-bearing for a board deck, and it has a
  hardcoded date filter from 2024.
- The warehouse bill triples because 200 dashboard cards each run an unoptimized query
  every 15 minutes.

The tool didn't fail. It just had no opinion about governance, and governance is not
something you can add to a 400-dashboard installation retroactively.

### 1.2 The "serious tool" tax

So the company buys Looker, or Power BI Premium, or Tableau. Now:

- Licensing costs scale per-seat, so the tool gets rationed. The people closest to the
  question can't access the data.
- Modeling requires a specialist. Every new metric is a ticket with a two-week queue.
- The tool is a closed system. Getting data *out* — into a Slack alert, a reverse-ETL
  sync, a notebook — means an API that was clearly an afterthought.
- Self-hosting is either impossible or a professional-services engagement.

### 1.3 The stitched-together stack

The sophisticated answer is to compose: dbt for modeling, Cube for the semantic layer,
Superset or Metabase for visualization, Airflow for orchestration, Monte Carlo for data
quality, a homegrown Slack bot for alerts, and now a vector database and an LLM wrapper
for "AI analytics".

This works. It also means seven systems, seven auth models, seven upgrade cycles, and a
lineage story that exists only in a Confluence diagram somebody drew in 2024. The
integration surface *is* the job, and it never ships.

### 1.4 The AI disappointment

Every BI vendor shipped an "AI" feature in the last two years. Almost all of them are the
same thing: pipe the table schema into an LLM, ask for SQL, run it, show the result.

This demos well and fails in production, because:

- The model doesn't know that `orders.status = 'complete'` excludes refunds, or that
  `users` contains 340,000 test accounts, or that revenue is recognized monthly.
- It produces a *different* number than the dashboard for the same question, which
  destroys trust in both.
- It has no permission model, so it either leaks data or is restricted into uselessness.
- It gives an answer with no way to verify it.

An AI analyst that isn't grounded in a semantic layer is a very expensive way to generate
plausible wrong numbers.

---

## 2. The insight

**These four problems are one problem.**

Governance, cost control, self-service, and trustworthy AI all require the same
foundation: **a single, versioned, permission-aware definition of what the business's
data means.** A semantic layer.

Most products treat the semantic layer as an enterprise upsell or a separate tool. Pivot
treats it as the spine.

- A chart is a query against the semantic layer.
- An alert is a threshold on a semantic layer metric.
- A flow materializes semantic layer models.
- The AI analyst reasons over the semantic layer, not over raw schemas.
- Row-level security is enforced *in* the semantic layer, so every consumer inherits it.

Build that spine correctly and everything else becomes a consistent, governed surface on
top of it. Skip it, and you are building Metabase again.

---

## 3. What Pivot is

Pivot is a single product with eight surfaces, all sharing one metadata model, one
permission system, and one semantic layer.

| Surface | What it does |
|---|---|
| **Connect** | 40+ warehouse, database, file, and SaaS connectors with schema sync, catalog, and profiling |
| **Model** | Git-versioned semantic layer: models, dimensions, metrics, joins, RLS policies. dbt-native |
| **Explore** | Drag-and-drop query builder for everyone; a genuinely good SQL IDE for analysts |
| **Visualize** | 40+ chart types, real pivot tables, conditional formatting, custom viz plugins |
| **Dashboard** | Composable dashboards with cross-filtering, drill-down, parameters, and tabs |
| **Alert** | Threshold, anomaly, and change-detection alerts routed to Slack, email, webhook, PagerDuty |
| **Flow** | Visual ELT and transformation pipelines with SQL/Python nodes, tests, and reverse-ETL |
| **Ask** | A grounded AI analyst — answers, explanations, root-cause analysis, and generated content |

---

## 4. Who it's for

Pivot optimizes for the **data-capable mid-market and the platform teams inside large
companies**: organizations with 50–5,000 employees that have a warehouse, have outgrown
spreadsheets, and refuse to pay per-seat rent to see their own data.

### Primary personas

**Maya — Analytics Engineer.** Owns the warehouse and the dbt project. Wants metrics
defined once, in Git, reviewed in PRs. Her failure mode is being a human API for every
"can you pull me…" request. Pivot wins by making her models directly consumable —
she publishes a metric, and it appears in everyone's explore UI, alerts, and AI answers.

**Daniel — Business Operations Lead.** Lives in dashboards, does not write SQL, is
smarter about the business than anyone in engineering. His failure mode is waiting four
days for a number. Pivot wins by giving him a query builder over governed models where
every field is already correct, and an AI that answers in seconds with the SQL shown.

**Priya — Head of Data Platform.** Accountable for cost, security, and uptime. Her
failure mode is discovering a $40k warehouse bill from a runaway dashboard, or an auditor
asking who accessed PII. Pivot wins on query governance, caching, audit logs, and SSO
that works on day one.

**Sam — Founder / Executive.** Wants three numbers, daily, correct, on their phone.
Pivot wins with subscriptions, mobile, and alerts that fire before they have to ask.

### Explicitly not the target (for now)

- Consumer-grade spreadsheet replacement. That's a different product.
- Real-time operational dashboards at sub-second refresh. We target seconds, not
  milliseconds. Grafana is excellent at this; we will not fight it.
- Regulated financial reporting with statutory certification workflows.

---

## 5. How Pivot wins

### Against Metabase
Metabase's genius is the 5-minute setup. We match it — single binary, embedded database,
zero config — and then don't hit a ceiling. Where Metabase has "models" as a convenience,
Pivot has a real semantic layer with metrics, joins, and RLS. Where Metabase's AI reads
your schema, Pivot's AI reads your business definitions.

### Against Superset
Superset is powerful and free, and its learning curve is a cliff. Semantic modeling is
per-chart, the permission model is confusing, and the setup requires a Python environment,
Celery, Redis, and patience. Pivot matches its chart depth with an interface a
non-engineer can use and an install that takes one command.

### Against Looker
LookML was right about everything except being proprietary, requiring a specialist, and
costing what it costs. Pivot's semantic layer is YAML, Git-native, dbt-compatible, and
authorable from the UI with an escape hatch to code. And it self-hosts.

### Against Power BI / Tableau
These win on desktop authoring depth and lose on collaboration, version control,
cloud-native deployment, and openness. We do not try to beat Tableau at pixel-level
visual authoring. We beat it at everything that happens after the chart exists.

### Against the DIY stack
We are not better than dbt + Cube + Superset + Airflow assembled by a strong platform
team. We are better than that stack assembled by a team that also has other work. One
system, one auth model, one upgrade, one lineage graph.

---

## 6. The wedge

Breadth is the end state, not the entry point. The initial wedge is narrow and sharp:

> **"Point Pivot at your dbt project and get a governed, AI-queryable BI layer in an
> afternoon."**

Companies running dbt already did the modeling work. Today that work is stranded — it
produces tables, and then a BI tool re-models those tables badly. Pivot imports dbt
models, metrics, tests, and docs directly into its semantic layer and makes them
immediately explorable, dashboardable, alertable, and AI-queryable.

That is a 30-minute demo with an undeniable payoff, and it lands us in the semantic layer
from day one — exactly where the rest of the product needs us to be.

---

## 7. Non-negotiable product commitments

These constrain every roadmap decision that follows.

1. **30 seconds to first query.** Download, run, connect, see data. No Docker Compose
   file required, no external dependencies.
2. **Every AI answer shows its work.** The generated SQL, the semantic model used, and
   the confidence. Always inspectable, always re-runnable.
3. **The same number everywhere.** A metric shown on a dashboard, in an alert, via the
   API, and in an AI answer is computed by the same compiled query. Divergence is a bug.
4. **Permissions are enforced at the query compiler, not in the UI.** There is no code
   path that returns data a user isn't entitled to, regardless of which surface asked.
5. **No per-seat pricing in the open core.** Viewers are free, forever. Charging companies
   to let their own employees look at their own data is the thing we are replacing.
6. **Your data never leaves your infrastructure.** Self-hosted means self-hosted,
   including AI — with local model support and provider-agnostic routing.
7. **Everything is an API.** The UI is the first client of the public API, not a
   privileged one.

---

## 8. What success looks like

| Horizon | Measure |
|---|---|
| **v0.1 (M4)** | An engineer connects Postgres, writes SQL, saves a chart, and shares it |
| **v0.5 (M9)** | A team runs its weekly business review entirely on Pivot dashboards |
| **v1.0 (M14)** | A company replaces Metabase or Superset in production, with governance they didn't have before |
| **v1.5 (M19)** | A non-technical operator gets a correct, trusted answer from Pivot AI without opening a dashboard |
| **v2.0 (M24)** | A SaaS company embeds Pivot as the analytics product their customers use |

The honest long-term test: **does a data team that adopts Pivot stop maintaining a
spreadsheet of "real" numbers on the side?** If the shadow spreadsheet dies, we won.

---

## 9. What we are deliberately not building

Scope discipline is the difference between a platform and a graveyard.

- **A data warehouse.** We query yours. DuckDB acceleration is a cache, not a system of
  record.
- **A general-purpose notebook.** Jupyter exists and is better. We integrate.
- **A full data catalog / governance suite.** We do lineage and documentation for what
  Pivot touches. DataHub and OpenMetadata do the enterprise-wide version; we integrate.
- **A CDP or customer data platform.** Reverse-ETL in Flows is a feature, not a product.
- **An ML training platform.** We serve predictions and run forecasts. We do not train
  your models.
- **A desktop application.** Web-first, always. Mobile is a companion app, not a port.

---

## 10. Next

- Technology decisions: [architecture/tech-stack.md](architecture/tech-stack.md)
- The build order: [roadmap/README.md](roadmap/README.md)
