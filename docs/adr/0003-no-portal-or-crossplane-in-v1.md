# ADR-0003: No developer portal or Crossplane in v1

**Date:** 2026-08-03 · **Status:** accepted

## Context

Two tools are conventional in this space and were considered for the paved road: **Backstage** (developer
portal, catalog, scaffolder) and **Crossplane** (declarative cloud-resource provisioning via CRDs). Both are
credible, both are widely adopted, and both would be defensible additions. Neither is in v1.

The v1 goal is narrow: cut environment provisioning from days to under an hour, self-served, with
guardrails. Every tool added is surface a small team has to run, upgrade, and be on-call for.

## Decision

**v1 ships the Go platform surface (`platform-api`, `environment-controller`, `platformctl`) and no portal
or Crossplane.**

- **No Backstage.** The API and CLI are the durable contract; a portal is a *presentation* layer over them. Building the portal first would mean designing the contract around a UI, and it front-loads a heavyweight service before we know whether teams want a UI or a CLI.
- **No Crossplane.** Environment provisioning is procedural (validate → render → PR → reconcile) and needs custom validation, adoption instrumentation, and PR workflow. That logic belongs in Go where it can be unit-tested, not distributed across compositions.

## Alternatives considered

- **Backstage first.** Best when the org's problem is *discovery* — many services, unclear ownership, no front door. Our problem is provisioning latency, so a portal would decorate the bottleneck rather than remove it.
- **Crossplane for environments.** Strong fit for composing cloud resources; weaker for a workflow needing request-time policy checks and PR orchestration. Also adds a second reconciliation model alongside our own controller, so failures have two places to hide.
- **Both, later.** Retained as options — see below.

## Consequences

- The Go surface stays the single contract, which keeps failure modes in one place and testing straightforward.
- **Ownership** (a v1 requirement) is met without a catalog: `Environment.spec.owner` is mandatory, and the controller can emit a generated ownership index. A catalog UI is not required to know who owns what.
- We give up the discovery/browse experience a portal provides. Accepted for v1; revisit when the number of environments makes "what exists and who owns it" a real question rather than a hypothetical one.
- **Revisit triggers, written down so this isn't re-litigated ad hoc:**
  - *Add Backstage* when teams ask "what exists / who owns this / where are the docs" often enough to be a support load — i.e. when the problem becomes discovery, not provisioning. It goes on top of the existing API; the contract does not change.
  - *Add Crossplane* when teams need **cloud resources** (databases, buckets, queues) alongside environments. That is a composition problem, and hand-rolling cloud provisioning in our controller would be reinventing it badly. The boundary would be: controller owns Kubernetes-native objects, Crossplane owns cloud resources, both reconciled from the same `Environment`.

## Note

Both tools are demonstrated in a sibling project (`backstage-crossplane`), including a working Crossplane v2
XRD/Composition. Excluding them here is a scoping decision for this platform's v1, not a gap in familiarity.
