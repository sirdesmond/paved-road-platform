# ADR-0001: Where Terraform stops and the platform controller starts

**Date:** 2026-08-03 · **Status:** accepted

## Context

Two provisioning mechanisms are in play: Terraform (foundational AWS infrastructure) and our own
Kubernetes controller (per-team environments). Without an explicit boundary, both drift toward doing
everything — the common failure being Terraform runs that take 40 minutes and are triggered by routine
team requests.

## Decision

**Terraform owns infrastructure whose lifecycle is measured in months. The controller owns objects whose
lifecycle is measured in days.**

- **Terraform:** accounts, VPCs and Transit Gateway attachments, EKS clusters and node groups, IAM/IRSA, remote state, bootstrapping Argo CD.
- **Controller:** namespaces, quotas, network policies, routes, observability defaults, SLOs — anything created per team, per environment, continuously.

Litmus test: **if a change is triggered by a team's request rather than a platform decision, it does not
belong in Terraform.**

## Alternatives considered

- **Terraform for everything** (teams run modules): needs distributed credentials, no continuous reconciliation, drift returns immediately, slow feedback.
- **Crossplane for everything** (including foundations): appealing consistency, but bootstrapping clusters from a controller that runs *on* a cluster is a chicken-and-egg problem, and cluster-lifecycle changes are exactly the rare, high-blast-radius operations where Terraform's plan/review cycle is a feature.

## Consequences

- Environment creation is fast and self-healing; cluster changes stay deliberate and reviewed.
- Two tools to know — accepted, because the boundary is explainable in one sentence.
- Some resources (e.g. a per-environment S3 bucket) sit near the line. Default: if a team asks for it and it's destroyed with the environment, it belongs to the controller. Revisit if that surface grows — Crossplane becomes the natural answer at that point.
