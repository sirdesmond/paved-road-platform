# ADR-0007: Tier is immutable

**Date:** 2026-08-05 · **Status:** accepted

## Context

`spec.tier` decides the resource ceiling, whether the environment expires, and (later) the rollout strategy
and approval requirements. It's the single field with the widest blast radius in the API.

Left mutable, promoting a staging environment to production is a one-field edit. That's either a convenient
feature or a way to acquire a production-sized quota, lose the TTL, and inherit production's guarantees
without anyone reviewing it.

## Decision

**Tier can be set at creation and never changed**, enforced by a CEL transition rule on the field:

```go
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="tier is immutable: create a new environment at the target tier and migrate to it"
```

Transition rules only evaluate on update, so creation is unaffected.

The reasoning is that "promote to production" isn't really a field edit. In any platform where the word
production means something, it involves a review, a migration, and usually a different name. Modelling it as
an edit makes a significant operation look trivial, and the CEL rule makes the platform's opinion explicit at
the point someone tries.

## Alternatives considered

**Mutable, with a policy gate on the transition.** Allow the change, but require an approval or restrict who
can make it. More flexible, and probably right for a larger organisation with the RBAC to express it. Rejected
here because `Environment` is cluster-scoped ([ADR-0005](./0005-environment-is-cluster-scoped.md)), so we
can't do per-team RBAC on it anyway — the gate would have to live in `platform-api`, which can be bypassed by
anyone with cluster access. An immutable field is enforced everywhere, by the API server.

**Mutable, no restriction.** Simplest, and quietly dangerous: a typo in a patch could hand a team a
production quota with no expiry and no trace beyond the audit log.

## Consequences

- Getting the tier wrong means deleting and recreating. Since the environment name is the namespace name, that also means tearing down whatever was in it — genuinely annoying if someone fat-fingers `dev` on their first try.
- Legitimate promotions require creating a new environment and migrating. That's the intended friction, but if it turns out to be a common workflow rather than a rare one, the friction is a bug and this decision is wrong.
- The rejection message has to carry the whole explanation, since there's no UI to explain it. It tells the user what to do instead, not just that they can't.
- One more thing enforced at the API server rather than in code we maintain, which is the direction of travel in [ADR-0004](./0004-policy-enforcement-layers.md).

## Revisit if

Teams promote environments often enough that recreate-and-migrate becomes a recognised chore. At that point
the right answer is probably an explicit promotion workflow in `platform-api` that creates the new environment
and tracks the migration — not making the field mutable.
