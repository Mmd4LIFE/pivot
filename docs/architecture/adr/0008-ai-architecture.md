# ADR-0008: Separate Python AI service; natural language compiles to semantic queries

**Status:** Accepted
**Date:** 2026-09-19

## Context

[The vision](../../vision.md#14-the-ai-disappointment) argues that most AI-BI features fail
the same way: pipe the schema to an LLM, ask for SQL, run it. This demos well and fails in
production because the model doesn't know the business's definitions, produces numbers that
disagree with dashboards, has no permission model, and offers no way to verify an answer.

Two decisions follow from trying to do better: where the AI code lives, and what the model
is actually asked to produce.

## Decision

**1.** A separate Python 3.12 FastAPI service owns all model interaction, embeddings,
forecasting, anomaly detection, and SQL analysis via SQLGlot. It communicates with the Go
control plane over gRPC and is **optional** — no core feature may hard-depend on it.

**2.** The model generates a **structured semantic query** — a selection of metrics,
dimensions, and filters from the governed semantic layer — which the Phase 3 compiler turns
into SQL. **The model never produces SQL** on the governed path.

## Rationale

### Why a separate Python service

The AI ecosystem is Python and will remain Python: model SDKs, embedding libraries,
forecasting (Prophet, statsmodels), and evaluation tooling. Reimplementing in Go means
permanently trailing.

Separate rather than embedded, for four reasons: AI load is spiky and GPU-adjacent so it
scales independently; it can be disabled entirely for air-gapped or AI-skeptical
deployments; it isolates a heavyweight dependency tree from the core; and core updates
don't require revalidating model behavior.

The single-binary story survives because AI is opt-in. `./pivot` is a complete BI platform;
AI features light up when the service is reachable and degrade to disabled — with a clear
message — when it isn't.

**SQLGlot lives here too**, which is the pragmatic compromise worth naming. It's the best
multi-dialect SQL parser and transpiler that exists, with no Go equivalent. Rather than
write one, we expose it over gRPC for dialect translation, validation, and lineage
extraction. The consequence to accept: those features degrade when the AI service is down,
so they must fail soft and never block a query.

### Why semantic queries instead of SQL

This is the central architectural bet of the product.

Text-to-SQL accuracy plateaus well below the threshold at which a business user should
trust an answer without checking it. Constraining the output to a selection from a curated
set of governed objects collapses the problem: the model picks from options rather than
composing arbitrary code.

Four consequences, each independently valuable:

- **Security.** Even a fully prompt-injected model cannot exceed the user's permissions,
  because the compiler applies RLS and masking regardless of what the model emitted. The
  model never produces anything executable.
- **Correctness.** The AI cannot invent a revenue definition. It can only use the one the
  business defined, so its answer matches the dashboard by construction — satisfying
  [principle 1.2](../../roadmap/00-principles.md#12-one-number-one-definition).
- **Verifiability.** Output is validated against permitted objects before anything runs.
  Malformed output fails fast instead of executing something subtly wrong.
- **Evaluability.** Structured output over a known object set is far easier to benchmark
  than free-form SQL, which is what makes the 95% gate in
  [Phase 7](../../roadmap/phase-7-ai.md) measurable at all.

Free-form SQL generation remains available as an explicitly-labeled fallback for
exploration, gated by the separate `native_query` permission.

### Model strategy

Provider-agnostic routing, defaulting to Anthropic's Claude family: **Claude Opus 5** for
the agentic analyst and multi-step reasoning, **Claude Sonnet 5** for standard NL→semantic
query and narrative generation, **Claude Haiku 4.5** for classification, routing, and
high-volume cheap calls. Bring-your-own-key for OpenAI, Google, Bedrock, and Azure. Local
models via Ollama or vLLM for air-gapped deployments.

Local model support is a hard requirement of the self-hosting commitment, and it's why the
prompting layer must not depend on provider-specific features.

## Alternatives considered

### AI embedded in the Go control plane
One less service. Rejected: it forces a Go reimplementation of the Python ecosystem, makes
AI a mandatory dependency, and couples AI iteration to core releases.

### NL → SQL directly
The industry-standard approach. Rejected on all four consequences above, most importantly
security: a model that emits SQL must be trusted, and trusting a model that ingests
attacker-controllable data (column values, descriptions) is not a defensible position in a
multi-tenant product.

### A hosted AI service we operate
Simpler for us, impossible for our users. Self-hosted means self-hosted, including AI.

### Fine-tuned per-customer models
Considered for accuracy. Deferred past v2.0: retrieval quality and the glossary deliver
most of the benefit at a small fraction of the cost and operational complexity.

## Consequences

**Positive**
- Permissions hold automatically for AI, with no additional enforcement code
- AI answers match dashboards by construction
- Accuracy is measurable, which makes the benchmark gate meaningful
- Air-gapped deployments get real AI
- The AI service scales and fails independently

**Negative**
- **A second runtime and deployment artifact.** Docker Compose and Helm get more complex
- **The AI is only as good as the semantic layer.** A company with a thin semantic layer
  gets a weak AI — which is honest, and is also why the AI phase follows the semantic layer
  phase, but it means AI quality varies by customer in a way we only partly control
- SQLGlot-dependent features (lineage from raw SQL, dialect translation) degrade when the
  service is down
- gRPC serialization adds latency, mitigated by Arrow zero-copy for data payloads
- Questions the semantic layer can't express must be refused. This is correct behavior and
  it will read as a limitation to users comparing against tools that answer everything —
  including when they're wrong

## Revisit if

- The benchmark shows free-form SQL generation materially outperforming semantic queries on
  a governed layer (which would be surprising and worth knowing)
- A Go-native SQL transpiler reaches SQLGlot's quality
- Local model capability closes the gap enough to make provider routing unnecessary
