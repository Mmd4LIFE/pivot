# Grafana

`pivot-overview.json` is a reference dashboard for a single Pivot instance. It is a
starting point, not a product surface — copy it and change it.

## Importing

Grafana → Dashboards → New → Import → Upload JSON file. It asks for two things:

- **Datasource** — any Prometheus datasource. The dashboard templates it as
  `$datasource` rather than hard-coding a UID, because a UID from my Grafana means
  nothing in yours.
- **Instance** — once data arrives, the `$instance` variable populates from
  `label_values(pivot_http_requests_total, instance)`. Every panel filters on it, so a
  dashboard pointed at three Pivots shows one at a time rather than their sum.

## Scraping

Metrics are on by default and served at `/metrics` on the same listener as the API:

```yaml
scrape_configs:
  - job_name: pivot
    static_configs:
      - targets: ["pivot:8080"]
```

The endpoint is unauthenticated. Prometheus cannot hold a session, and making every
scrape cost an Argon2 verification would be a denial of service with a cron schedule.
It exposes request counts, durations and in-flight totals — no identifiers, no
query text, no tenant names. If that is still more than you want on your network,
bind Pivot's listener where only Prometheus can reach it, or put the endpoint behind
your ingress' own auth.

To turn it off entirely, set `PIVOT_METRICS_ENABLED=false`; the route then does not
exist and a scrape gets a 404, which is the truth rather than an empty 200.

## What the panels show

| Panel | Query | Why |
|---|---|---|
| Request rate | `rate(pivot_http_requests_total[5m])` by route | Where the traffic is |
| Error rate | the same counter, 5xx and 4xx as a fraction of the total | Errors are a **label**, not a second counter, so this panel and the one above it cannot disagree |
| Latency | p50 / p95 / p99 from `pivot_http_duration_seconds_bucket` | p99 is the one users complain about |
| In flight | `pivot_http_in_flight` | Distinguishes "slow" from "stuck": a rising in-flight count with flat throughput is saturation |

The route label is the router's **pattern** (`/api/v1/auth/sessions/{id}`), never the
path, so series count is bounded by the number of routes. Requests that match nothing
fall into the catch-all. A scanner cannot create a time series per guess.

## Keeping it honest

`internal/observability/dashboard_test.go` parses this file and checks every metric and
label it queries against a real exposition from the running code. A reference dashboard
rots in exactly one way — something gets renamed and the panels go quietly empty — and
that test is what fails instead.
