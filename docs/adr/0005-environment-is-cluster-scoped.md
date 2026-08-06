# ADR-0005: Environment is a cluster-scoped resource

**Date:** 2026-08-04 · **Status:** accepted

## Context

An `Environment` provisions a namespace, and everything inside it (quota, network policy, routes). The CRD
could be namespaced, which is the more common default and gives you per-namespace RBAC for free, or
cluster-scoped.

The deciding factor turned out to be garbage collection rather than anything to do with tenancy.

Kubernetes doesn't allow a cluster-scoped object to be owned by a namespaced one. If you set that owner
reference anyway, it isn't rejected: the dependent is treated as having an unresolvable owner, it's excluded
from garbage collection, and you get an `OwnerRefInvalidNamespace` event that nobody is watching for.

So a namespaced `Environment` cannot own the `Namespace` it creates. Delete the `Environment` and the
namespace stays behind, along with everything in it, and nothing errors.

## Decision

`Environment` is cluster-scoped (`+kubebuilder:resource:scope=Cluster`).

That makes it a valid owner for the namespace, so ordinary garbage collection handles teardown: delete the
`Environment` and Kubernetes removes the namespace and, transitively, everything inside it.

## Alternatives considered

**Namespaced `Environment` plus a finalizer.** Keep the CRD namespaced and delete the namespace by hand in a
finalizer. This works, and plenty of controllers do it. Rejected because it reimplements garbage collection
badly: the finalizer has to be correct under interruption, ordering and partial failure, and a bug means
objects stuck in `Terminating` that require manual intervention. We'll need finalizers eventually for
resources outside the cluster, but using one to do a job the GC already does correctly is the wrong trade.

**Namespaced `Environment` that doesn't own the namespace at all**, with cleanup driven by a reaper watching
for orphans. More moving parts, and a window where a deleted `Environment` leaves a live namespace.

## Consequences

- Teardown is handled by Kubernetes rather than by our code, which is the single largest reduction in failure modes available to this controller.
- **RBAC gets coarser, and this is the real cost.** Cluster-scoped resources can't be granted per-namespace, so we can't hand a team RBAC to manage only their own `Environment`s. Access control moves up a layer: `platform-api` decides who may request what, and direct cluster access is restricted (see [RFC-0001](../rfc/0001-self-service-environments.md)). Acceptable here because the API was always going to be the front door, but it would be a problem on a platform where teams work against the cluster directly.
- Environment names are globally unique. Fine, since they map to namespaces, which are too — but it means naming needs a convention (`<team>-<tier>`) rather than relying on namespace separation.
- Same conclusion the Crossplane `XAppSpace` in the sibling repo reached, for the same reason. The rule generalises: **if your custom resource creates cluster-scoped objects, it has to be cluster-scoped itself.**

## Revisit if

`Environment` stops creating namespaces — for instance if environments become something that lives *inside* a
pre-existing namespace. At that point namespaced scope becomes both possible and preferable, since it would
buy back per-team RBAC.
