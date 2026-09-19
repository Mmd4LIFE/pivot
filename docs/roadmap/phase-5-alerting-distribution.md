# Phase 5 — Alert & Distribute

**Months:** M11–M13 · **Effort:** 38 ew · **Team:** 8 · **Release:** **v0.9**

---

## Goal

Make Pivot reach people who never open it. Alerts that fire when something changes,
subscriptions that arrive before anyone asks, and delivery to the places work actually
happens.

The strategic point: most people in a company will never log into a BI tool. A BI tool that
only serves the people who visit it serves maybe 15% of the organization.

## Exit criteria

- [ ] 100 alerts run for 30 days with zero missed fires and zero false deliveries
- [ ] An alert respects the recipient's RLS — two recipients of the same alert see
      different numbers when their policies differ
- [ ] Email renders correctly in Outlook, Gmail, and Apple Mail, light and dark
- [ ] A scheduled report with 30 cards generates a PDF in under 60 seconds
- [ ] Alert evaluation for 10,000 alerts completes within its window at p99

---

## 1. Alert engine

| ID | Feature | Pri | Size |
|---|---|---|---|
| P5-ALT-001 | Threshold alerts (above, below, between, outside, equals) | P0 | L |
| P5-ALT-002 | Change alerts (absolute or percentage vs. a prior period) | P0 | M |
| P5-ALT-003 | Goal alerts (metric crosses a target) | P0 | S |
| P5-ALT-004 | Result-not-empty / result-empty alerts | P0 | S |
| P5-ALT-005 | Multi-condition alerts with AND/OR composition | P1 | M |
| P5-ALT-006 | **Anomaly alerts** (statistical deviation from expected) | P0 | XL |
| P5-ALT-007 | Freshness alerts (data hasn't updated) | P0 | M |
| P5-ALT-008 | Schema-change alerts | P1 | M |
| P5-ALT-009 | Per-row alerting (fire once per breaching entity) | P1 | L |
| P5-ALT-010 | Alert on a semantic metric, not just a saved question | P0 | M |
| P5-ALT-011 | Custom SQL alerts | P1 | S |

**P5-ALT-006 is XL and is the phase's differentiator.** Threshold alerts require someone to
know the threshold, which means they're only set for things people already watch. Anomaly
detection catches what nobody thought to watch. The implementation lives in the Python AI
service: seasonal decomposition (STL) for series with weekly/daily patterns, robust
z-scores for stationary series, and changepoint detection for level shifts. Every anomaly
alert reports *why* it fired — expected range, observed value, and the model used —
because an unexplained anomaly alert gets muted within a week.

### Alert control

| ID | Feature | Pri | Size |
|---|---|---|---|
| P5-ACT-001 | Evaluation schedules (cron, interval, on data refresh) | P0 | M |
| P5-ACT-002 | Alert states: OK, pending, firing, resolved, error | P0 | M |
| P5-ACT-003 | Hysteresis: fire after N consecutive breaches | P0 | M |
| P5-ACT-004 | Cooldown and re-notification intervals | P0 | M |
| P5-ACT-005 | Snooze and mute with expiry | P0 | S |
| P5-ACT-006 | Maintenance windows | P1 | M |
| P5-ACT-007 | Alert grouping and deduplication | P1 | M |
| P5-ACT-008 | Alert history with evaluated values | P0 | M |
| P5-ACT-009 | Test-fire an alert before saving | P0 | S |
| P5-ACT-010 | Auto-disable after repeated evaluation errors, with notification | P0 | S |

**P5-ACT-003 and 004 are what separate an alerting system from a notification spammer.** A
metric oscillating around a threshold generates 50 alerts an hour without hysteresis, and
the recipient creates a mail filter. Alert fatigue is the failure mode; these are the
defenses. Defaults are conservative: fire after 2 consecutive breaches, re-notify at most
hourly.

## 2. Delivery channels

| ID | Channel | Pri | Size |
|---|---|---|---|
| P5-CHN-001 | Email (HTML with embedded charts, plus plain text) | P0 | L |
| P5-CHN-002 | Slack (rich blocks, chart images, threaded updates) | P0 | L |
| P5-CHN-003 | Microsoft Teams (adaptive cards) | P0 | M |
| P5-CHN-004 | Webhook (signed, retried, with delivery log) | P0 | M |
| P5-CHN-005 | PagerDuty | P1 | S |
| P5-CHN-006 | In-app notification | P0 | S |
| P5-CHN-007 | SMS via Twilio | P2 | S |
| P5-CHN-008 | Discord | P2 | S |
| P5-CHN-009 | Google Chat | P2 | S |
| P5-CHN-010 | Custom channel plugin interface | P2 | M |

### Routing

| ID | Feature | Pri | Size |
|---|---|---|---|
| P5-RTE-001 | Multi-recipient with per-recipient channel preference | P0 | M |
| P5-RTE-002 | Routing rules by severity, tag, or condition | P1 | M |
| P5-RTE-003 | Escalation policies (notify X, then Y after N minutes) | P1 | L |
| P5-RTE-004 | On-call schedule integration | P2 | M |
| P5-RTE-005 | Per-user quiet hours and timezone awareness | P1 | M |
| P5-RTE-006 | Delivery log with retry and failure visibility | P0 | M |

## 3. Subscriptions & reports

| ID | Feature | Pri | Size |
|---|---|---|---|
| P5-SUB-001 | Dashboard subscriptions on a schedule | P0 | L |
| P5-SUB-002 | Question subscriptions with results inline | P0 | M |
| P5-SUB-003 | PDF report generation with a designed layout | P0 | L |
| P5-SUB-004 | Excel export with one sheet per card | P0 | M |
| P5-SUB-005 | Personalized subscriptions (each recipient's own RLS view) | P0 | L |
| P5-SUB-006 | Filter overrides per subscription | P0 | M |
| P5-SUB-007 | Conditional delivery ("only send if there's something to say") | P1 | M |
| P5-SUB-008 | Self-service subscribe/unsubscribe | P0 | S |
| P5-SUB-009 | Report templates with branding | P1 | M |
| P5-SUB-010 | Burst reports (one report per value of a dimension) | P1 | L |
| P5-SUB-011 | Attachment size limits with a link fallback | P0 | S |

**P5-SUB-005 is where alerting meets security, and it's the item most likely to leak
data.** A dashboard emailed to 40 people must render 40 times, once per recipient's
permissions — not once as the author. The naive implementation renders once and blows the
entire RLS model in a single feature. The engine renders per unique *policy set* (not per
user), which keeps it tractable: 40 recipients under 3 distinct policy sets means 3
renders. This is also why alerts had to come after
[Phase 4](phase-4-collaboration-governance.md).

**P5-SUB-007 fights inbox blindness.** A daily report that says "nothing changed" 300 times
a year trains people to delete it unread, including on the day it mattered.

## 4. Rendering pipeline

| ID | Feature | Pri | Size |
|---|---|---|---|
| P5-RND-001 | Headless Chromium worker pool for chart and dashboard rendering | P0 | L |
| P5-RND-002 | Chart-to-image (PNG, SVG) from the same React components | P0 | M |
| P5-RND-003 | Dashboard-to-PDF with pagination and page breaks | P0 | L |
| P5-RND-004 | Email-safe HTML templates (table layout, inline CSS) | P0 | L |
| P5-RND-005 | Dark-mode-aware email rendering | P1 | M |
| P5-RND-006 | Render queue with priority, timeout, and retry | P0 | M |
| P5-RND-007 | Render cache for identical content | P1 | M |
| P5-RND-008 | Font embedding and i18n/RTL support in rendered output | P1 | M |

**P5-RND-004 is worse than it sounds.** Email HTML is 1999 technology: table layouts,
inline CSS, no flexbox, and Outlook's Word rendering engine. Getting a dashboard to look
professional across Outlook, Gmail, and Apple Mail in both light and dark mode is genuinely
multi-week work, and it's the first thing an executive sees. Budget accordingly. Tested
against a real client matrix, not a preview tool.

## 5. Scheduler

| ID | Feature | Pri | Size |
|---|---|---|---|
| P5-SCH-001 | Distributed scheduler with leader election | P0 | L |
| P5-SCH-002 | Cron and interval schedules with timezone and DST correctness | P0 | M |
| P5-SCH-003 | Event-triggered schedules (on data refresh, on flow completion) | P0 | M |
| P5-SCH-004 | Execution isolation: one failure can't block the queue | P0 | M |
| P5-SCH-005 | Backfill and manual re-run | P1 | M |
| P5-SCH-006 | Jitter and load spreading | P0 | S |
| P5-SCH-007 | Schedule monitoring UI (upcoming, running, failed) | P0 | M |
| P5-SCH-008 | Missed-execution detection and recovery policy | P0 | M |

**P5-SCH-006 prevents the self-inflicted outage.** Everyone schedules their report for
9:00 AM. Without jitter, 500 dashboards refresh simultaneously, the warehouse queues, and
every report is late. Schedules are spread deterministically within their window.

**DST correctness (P5-SCH-002) is the classic scheduler bug.** "Daily at 2:30 AM" happens
twice on one day a year and zero times on another. The policy is explicit and documented
per schedule, not accidental.

---

## Technical notes

### Alerts run as someone
Every alert has an owning identity, and the query is compiled with that identity's
permissions. If the owner loses access to the underlying model, the alert fails visibly and
notifies an admin rather than silently continuing with stale permissions or running
unrestricted. Personalized subscriptions compile per recipient policy set.

### Evaluation must be cheap
10,000 alerts evaluating every 5 minutes is 2M queries a day if done naively, which would
be the most expensive feature in the product. Mitigations: alerts on the same model and
schedule share a single query; results come from the cache when freshness allows; anomaly
models are trained on a slow schedule and only scored on the fast one; and alerts on
accelerated models never touch the warehouse at all.

### Exactly-once delivery is the target
An alert delivered twice erodes trust; an alert never delivered is a failure. Delivery uses
a transactional outbox: the state transition and the delivery record commit atomically, and
a worker drains the outbox with idempotency keys. At-least-once delivery plus
deduplication at the channel is the practical guarantee.

---

## Explicitly deferred

| Deferred | To | Why |
|---|---|---|
| AI root-cause analysis on alerts | Phase 7 | Needs the agentic analyst |
| Alert-to-flow triggers | Phase 6 | Needs the flow engine |
| Mobile push notifications | Phase 10 | Needs the mobile app |
| Incident management | — | Not our product; PagerDuty integration is the answer |
| Alert auto-remediation | — | Out of scope |

---

## Risks

| Risk | Mitigation |
|---|---|
| Alert fatigue makes the feature worthless | Hysteresis, cooldown, and grouping are P0 with conservative defaults; measure mute rate as a product health metric |
| Anomaly detection has too many false positives | Ship in shadow mode first — detect and log without notifying — and tune against real data before enabling |
| Email rendering consumes the phase | Timeboxed; fall back to a simpler template with a "view in Pivot" link if the deadline is at risk |
| Personalized subscriptions leak data across recipients | Render per policy set with an explicit test asserting isolation; treat any leak as P0 |
| Scheduler load spikes take down the warehouse | Jitter, per-connection concurrency caps, and query budgets from Phase 4 |
| Chromium workers are a memory and reliability problem | Pool with hard limits, recycle after N renders, isolate in their own deployment |
