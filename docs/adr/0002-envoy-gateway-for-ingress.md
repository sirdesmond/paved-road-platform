# ADR-0002: Envoy Gateway (Gateway API) for ingress

**Date:** 2026-08-03 · **Status:** accepted

## Context

Environments need ingress by default: a hostname, TLS, and isolation between tenants. The legacy Ingress
API is effectively frozen, and its per-controller annotation sprawl makes "safe defaults" hard to express
or enforce. We also need a routing layer that can support multi-region failover later without a rewrite.

## Decision

Use **Envoy Gateway** implementing the **Gateway API**, with a clear ownership split:

- **Platform owns `Gateway` + `GatewayClass`** — listeners, TLS, and the shared entry point.
- **Teams own `HTTPRoute`** — their own routing rules, within a namespace they control.

This split is the point: Gateway API's role separation maps exactly to the platform/tenant boundary, which
the old Ingress API could only approximate with annotations and conventions.

## Alternatives considered

- **ingress-nginx:** familiar and widely deployed, but annotation-driven config resists safe defaults, and the tenancy story is weaker. Legacy Ingress is in maintenance.
- **A service mesh gateway (Istio):** capable, but pulls in mesh adoption as a prerequisite for basic ingress — too much surface for the problem at hand. Revisit if we adopt a mesh for other reasons.
- **AWS ALB Controller alone:** good AWS integration, but ties routing semantics to a cloud primitive and weakens portability of the platform contract.

## Consequences

- Gateway API is the modern, portable target; teams write a small, well-specified resource.
- Multi-region traffic policy has a natural home later.
- Cost: Gateway API is less familiar to teams than Ingress — mitigated by the controller generating routes automatically, so most teams never write one by hand.
- Envoy operational knowledge becomes a platform-team requirement (runbooks, upgrade path).
