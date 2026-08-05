# paved-road-platform

An internal developer platform built to answer one question:

> **Why does standing up a new application environment take days instead of hours — and what has to be true for a team to do it themselves, safely, without us?**

This is a hands-on engineering lab, not a demo app. The deliverables are the ones a platform team is
actually judged on: **self-service systems with clear ownership, safe defaults, strong guardrails, and
adoption you can measure** — plus the RFCs, ADRs, runbooks, and SLOs that make them real.

**The thesis:** the easy path has to be the safe path. If shipping correctly is slower than shipping
carelessly, teams route around the platform and you're back to expert-driven support.

## The headline metric

Everything here is organized around moving one number, because a platform with no measured outcome is a
hobby:

| | Before (expert-driven) | Target (paved road) |
|---|---|---|
| **New application environment** | days, gated on a platform expert | **< 1 hour, self-served** |
| **New region** | a project | a config change + a pipeline run |
| **Provisioning requests needing the platform team** | ~all of them | **< 20%**, trending down |
| **Change failure rate / rollback time** | unmeasured | tracked, with progressive rollout |
| **Alert → context in on-call's hands** | manual archaeology | assembled automatically |

Adoption is a first-class metric, not an afterthought: if teams don't use the paved road, the paved road
is wrong.

## What this demonstrates

- **Platform APIs in Go** — a provisioning API, a Kubernetes controller, and a CLI that share one contract.
- **Terraform foundations** — multi-account, multi-region AWS with EKS, IRSA, and remote state.
- **GitOps delivery** — Argo CD app-of-apps, ApplicationSets per environment/region, and the continuous-deployment flow with progressive rollout and real testing.
- **Traffic** — Envoy Gateway / Gateway API ingress, routing, and cross-region connectivity.
- **Reliability** — SLOs, alerting, runbooks, blameless postmortems, and on-call health as a design target.
- **Staff-level practice** — ambiguous problems turned into RFCs, decisions recorded as ADRs, contracts documented for the teams that consume them.

## Start here

| Doc | What it gives you |
|---|---|
| **[ROADMAP.md](./ROADMAP.md)** | The phased build plan, each phase with a demoable "done when." |
| **[ARCHITECTURE.md](./ARCHITECTURE.md)** | The design: layers, the self-service ↔ guardrail contract, and the Go platform surface. |
| **[docs/rfc/0001-self-service-environments.md](./docs/rfc/0001-self-service-environments.md)** | The worked RFC behind the core capability — the artifact that turns an ambiguous problem into something an org can rally around. |
| **[docs/adr/](./docs/adr/)** | Decision records (why Terraform stops where it does, why Envoy Gateway). |
| **[docs/runbooks/](./docs/runbooks/)** | On-call artifacts, including the postmortem template. |
| **[docs/worked-examples/](./docs/worked-examples/)** | Build-along track. Start at 01 and you end up with the controller, the delivery flow and the API, built by hand. |

## Repo layout

```
paved-road-platform/
├── terraform/          # multi-account, multi-region AWS: network, EKS, IRSA, state
│   ├── modules/        #   network · cluster-eks · identity · cross-account
│   └── accounts/       #   per-account/region roots
├── gitops/             # everything Argo CD reconciles
│   ├── bootstrap/      #   app-of-apps root + ApplicationSets (per env, per region)
│   └── platform/       #   argo-rollouts · envoy-gateway · policy · observability
├── platform-api/       # Go: the provisioning API (opens PRs; Git stays the write path)
├── environment-controller/  # Go: the Environment CRD + reconciler
├── platformctl/        # Go: CLI over the same contract
├── environments/       # the declarative result — what teams own
└── docs/               # rfc/ · adr/ · runbooks/ · slo/
```

## Sibling repos

| Repo | Focus |
|---|---|
| `paved-road-platform` (this) | Developer platform at scale: paved roads, Go platform APIs, multi-region, reliability |
| `backstage-crossplane` | The same platform ideas applied to AI/GPU infrastructure (Crossplane, model serving) |
| `ai-infra-lab` | The underlying systems-engineering skills ladder (Go controllers, Raft, eBPF/Rust) |

## Status

Design baseline: charter, roadmap, architecture, RFC-0001, and the ops templates. **Next:** Phase 0 —
Terraform for the multi-tenant EKS foundation. See [ROADMAP.md](./ROADMAP.md).
