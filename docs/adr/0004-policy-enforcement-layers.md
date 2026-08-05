# ADR-0004: Where guardrails are enforced

**Date:** 2026-08-03 · **Status:** accepted

## Context

"Guardrails" is the load-bearing word in this platform, so it needs an actual implementation rather than a
box on a diagram. The reflex answer is to install Kyverno and write policies. That was the right answer for
years; it isn't automatically the right one now.

Kubernetes 1.30 made **ValidatingAdmissionPolicy** GA, and 1.36 (April 2026) made **MutatingAdmissionPolicy**
GA. Both run in-process using CEL, with no webhook to time out, fail open, or upgrade. For a small team
carrying on-call, a component you don't run is worth a lot.

The other thing worth stating plainly: admission control is the *last* place to catch a problem. By then the
engineer has already written the config, opened a PR and waited for a sync. A rejection at that point is a
bad experience even when it's correct.

## Decision

Guardrails are enforced at three layers, deliberately, with different jobs.

**1. Request time, in `platform-api` (Go).** The primary layer, and the one teams actually experience.
Validates the request before a PR exists: naming, ownership, capacity headroom, tier rules, quota sanity.
This is where good error messages live — "you asked for 64 CPUs, your team's budget is 32, here's who to ask"
beats any admission rejection. Written in Go, unit tested, and fast.

**2. Admission, using native ValidatingAdmissionPolicy and MutatingAdmissionPolicy.** The backstop for
anything that didn't come through the API, including hand-applied manifests and mistakes in our own
controller. CEL policies for the structural rules: resource limits present, no `:latest`, required
owner/cost-centre labels, approved registries, no privileged containers. MAP injects the safe defaults.

No extra controller to install, upgrade, or be paged for, and it can't fail open the way a webhook can.

**3. Kyverno, only where CEL can't reach.** CEL can't make external calls or hold state, which rules out two
things we actually want:

- **Image signature and attestation verification** (cosign / sigstore). This needs to fetch and verify against a registry.
- **Cross-resource lookups**, where a decision depends on the state of something else in the cluster.

When we add supply-chain verification, Kyverno (or sigstore's policy-controller) comes in for that narrow
job. Not for the structural rules native policy already handles.

Note that we don't need Kyverno's `generate` at all: the `environment-controller` already creates the
namespace, quota and network policy as part of reconciling an `Environment`. Generating them again from a
policy engine would mean two things owning the same objects.

## Alternatives considered

- **Kyverno for everything.** One consistent policy language and a good UX. Rejected as the default because it means running, upgrading and being on-call for a webhook to do things the API server now does natively. It also puts a second reconciler alongside our controller. Still the right answer for teams without their own controller, or on older clusters.
- **OPA/Gatekeeper.** Rego is powerful and widely deployed, but it's a language the whole team would have to learn for rules that are mostly simple. Harder to justify now that CEL is built in.
- **Admission only, no request-time checks.** Simplest to build, worst to use. Every failure would arrive after the PR, as a sync error, with a message written for a machine.
- **Request-time only, no admission.** Fast and friendly, but trivially bypassed by anyone with cluster access, which makes it a suggestion rather than a guardrail.

## Consequences

- Teams get fast, readable feedback at request time, and there's still a hard backstop underneath. The two layers have different audiences: humans, then the system.
- Rules exist in two places (Go validation and CEL policy). That's real duplication and the main cost of this decision. Mitigation: the CEL policies cover the *structural* invariants and the Go layer covers *contextual* ones (budgets, capacity, ownership), so the overlap is small and each has a clear reason to exist. Worth revisiting if they start drifting.
- We depend on a recent Kubernetes version for MAP. Acceptable on managed EKS; would be a blocker on an older cluster, and that's the condition that would push us back toward Kyverno for the whole job.
- Adding Kyverno later for signature verification is additive. Nothing here has to be undone.

## Revisit if

- We end up writing enough CEL that a proper policy language with testing and reporting tooling becomes worth the operational cost.
- We need policy reports and audit dashboards across the fleet, which Kyverno does well and native policy doesn't really address.
- We have to support clusters older than the MAP GA line.
