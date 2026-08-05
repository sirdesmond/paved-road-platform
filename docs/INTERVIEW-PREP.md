# Interview prep — mapping this project to a Staff platform role

> Working notes, not part of the project's public story. The repo itself reads as a genuine engineering
> lab; this file is where you connect it to a specific job spec.

## Responsibility → where it shows up

| What the role asks for | Where this project demonstrates it | Honest status |
|---|---|---|
| Turn ambiguous infra problems into proposals, drive via RFCs | [RFC-0001](./rfc/0001-self-service-environments.md), ADR-0001/0002 | ✅ written — strongest artifact |
| Self-service capabilities + platform APIs **in Go** | `platform-api`, `environment-controller`, `platformctl` (Phase 2) | ⚠️ designed, not built — **highest priority to build** |
| Delivery standards: Terraform, GitOps/Argo CD, progressive rollout, testing, CD | Phases 0–1 | ⚠️ partially proven (GitOps loop works in sibling repo) |
| Multi-tenant EKS: reliability, security, scale, cost | Phase 0 + tenancy model in ARCHITECTURE §3 | ⚠️ designed |
| Envoy Gateway ingress, traffic routing | Phase 3, [ADR-0002](./adr/0002-envoy-gateway-for-ingress.md) | ⚠️ decided, not built |
| Multi-region, cross-account connectivity | Phase 0 + ARCHITECTURE §5 | ⚠️ designed |
| SLOs, alerting, incident follow-up (Grafana Cloud) | Phase 4, runbook + postmortem template | ⚠️ templates exist |
| AI-assisted ops, with healthy skepticism | Phase 5 + ARCHITECTURE §7 (explicit boundary table) | ✅ position articulated |
| On-call ownership and improving on-call health | Runbooks, postmortem template, alert-quality review in Phase 4 | ✅ practice defined |
| Cross-team alignment, clear written communication | The RFC/ADR set is itself the evidence | ✅ |

## The three things to build first (biggest signal per hour)

1. **`environment-controller` in Go.** A real controller-runtime reconciler for the `Environment` CRD. This is the single most load-bearing artifact — it proves Go, Kubernetes internals, and platform API design at once.
2. **`platform-api` + PR flow.** Shows the judgment call that self-service doesn't mean abandoning audit/revert.
3. **The Argo CD ApplicationSet fan-out** proving "a new region is a variable change." That's their stated roadmap goal, demonstrated.

## Talking points worth rehearsing

- **Why PR-based self-service over direct provisioning.** Audit + diff + revert are what make it safe to hand out; auto-merge for non-prod removes the friction. (RFC-0001, "Why PR-based.")
- **The Terraform/controller boundary**, with the one-sentence litmus test: *if a change is triggered by a team's request rather than a platform decision, it doesn't belong in Terraform.* (ADR-0001.)
- **How you'd close the old path.** The paved road must be measurably faster *before* you restrict direct access — rollout Phase D. Getting this backwards is how platform teams become resented.
- **Where AI-assisted ops earns its place**: context assembly and recommendations, never unattended production mutation, everything logged and attributable. Have the boundary table ready.
- **Why the policy engine isn't Kyverno by default** ([ADR-0004](./adr/0004-policy-enforcement-layers.md)). Good current-knowledge signal: ValidatingAdmissionPolicy went GA in 1.30 and MutatingAdmissionPolicy in 1.36, so native CEL policy handles the structural rules in-process with no webhook to run or fail open. Kyverno comes in only for image signature verification, because CEL can't make external calls. Then the design point underneath it: admission is the *last* place to catch a problem, so most of the guardrail work lives at request time in the Go API where the error message can actually help someone.
- **Adoption as the real metric.** "If teams don't use the paved road, the paved road is wrong" — and how you'd instrument it from day one.
- **Why no portal and no Crossplane in v1** ([ADR-0003](./adr/0003-no-portal-or-crossplane-in-v1.md)). Strong Staff answer: a portal decorates a provisioning bottleneck rather than removing it, and environment provisioning is procedural logic that belongs in testable Go. Then name the revisit triggers — that's what separates "I scoped this" from "I didn't get to it." If asked whether you know them, point at the sibling repo where you ran Crossplane v2 and debugged real CRD failures.
- **A real debugging story.** The Kyverno CRD annotation-size incident from the sibling repo: symptom (controller crashloop), false leads (assumed a missing chart), the tell (only the largest CRDs missing → size limit, not rendering), fix (server-side apply), prevention (default SSA for CRD-bearing charts). Staff interviews want the reasoning, including the wrong turns.

## Gaps to be honest about

- No production-scale operational experience *with this specific stack* — compensate by being precise about what you built vs. designed. Never imply the designed parts are running.
- Envoy Gateway and Grafana Cloud are decided-but-unbuilt here; say so plainly and describe how you'd validate the choices.
- The multi-region story is architectural until a second region actually exists. "Adding a region should touch a variable file and a generator" is a claim you should be ready to either demonstrate or label as a design intent.
