# ADR-0006: Tier defaults resolved in the controller, not a webhook

**Date:** 2026-08-05 · **Status:** accepted

## Context

`spec.tier` is meant to drive the resource ceiling: a dev environment shouldn't get the same quota as
production. CRD schema defaults can't express that, because they're constants — they can't branch on the
value of another field. So the defaulting has to happen somewhere else, and there are three candidates:

- a **mutating admission webhook**, filling in the spec before it's stored;
- the **controller**, resolving at reconcile time;
- **`platform-api`**, filling it in before the object is created.

There's a related question with the same shape: validation. Rejecting `ttl` on a production environment
could be a validating webhook or a CEL rule in the CRD schema.

Both questions come down to the same thing: is it worth running a webhook?

## Decision

**The controller resolves tier defaults, and publishes the result in `status.effectiveResources`. Validation
is CEL rules on the CRD. No webhook.**

A single pure function, `effectiveResources()`, merges the team's overrides field by field over the tier's
default. Both the quota and the status field are computed from it, so they can't disagree.

This follows the rule already set by `spec.ttl` and `status.expiresAt`: **spec is what the team asked for,
status is what the platform decided.** Nothing is written back into the user's fields.

CEL rules (`+kubebuilder:validation:XValidation`) are enforced by the API server itself, so they can't be
bypassed and there's nothing to run or keep available.

To make this work, the schema defaults on `resources` had to be removed. With them in place the API server
filled the field in before the controller ever saw the object, making "the team didn't specify" and "the team
asked for exactly 4" indistinguishable.

## Alternatives considered

**A mutating (defaulting) webhook.** The genuine advantage is that the stored object is complete: `kubectl get
env -o yaml` shows a team precisely what they got, and anything reading the spec gets the real values without
knowing about tiers. That's a real benefit and the main argument against this ADR.

Rejected for now on operational cost. A webhook is a service in the request path with certificates to rotate
and a failure policy to choose, and neither answer to "what happens when it's down" is good: `Fail` blocks all
environment creation, `Ignore` admits objects with no defaults, which is worse because it's silent. For a
small team carrying on-call, that's a lot to take on so that a field appears in the spec rather than the
status.

Running one locally is also awkward — the API server has to reach the developer's machine over trusted TLS —
which slows down every future change to the API.

**Defaults in `platform-api`.** Good developer experience and the natural home for budget context, but it only
applies to objects created through the API. Anything applied directly skips it, so it can't be the only
mechanism. It will likely *also* do this later for better error messages; that's additive, not a replacement.

**Validating webhook instead of CEL.** Everything needed here is expressible in CEL. A webhook would only be
justified by a rule requiring external calls or cross-resource lookups — the same boundary drawn in
[ADR-0004](./0004-policy-enforcement-layers.md), one level down.

## Consequences

- No new failure domain. Nothing to deploy, no certificates, no availability question.
- Consistent with the existing spec/status split, so the API has one rule rather than two conventions.
- **The main cost: the spec no longer tells you the effective ceiling.** Anything wanting the real numbers must read `status.effectiveResources`. That's an extra hop, and it's the thing a webhook would have solved. Documented on the field so nobody has to work it out.
- Defaults apply at reconcile time, so there's a brief window after creation where status is empty. Fine for a quota; would not be fine for anything security-relevant, which is a good reason to keep security defaults in admission policy rather than here.
- Removing the schema defaults doesn't retroactively clean existing objects: environments created earlier have `4 / 8Gi / 20` **stored** in their spec and will read as explicit overrides. They keep their old ceiling until someone edits them. Acceptable — but it's a real migration, not a no-op.
- Changing a tier default changes every environment that didn't override, on their next reconcile. That's the intent, but it means editing the table is a fleet-wide change and should be treated like one.

## Revisit if

- `platform-api` or another component needs the resolved values without reading status. That's the trigger to move defaulting into a webhook, and this ADR gets superseded rather than amended.
- We need defaults guaranteed at admission for something that matters before the first reconcile.
- The CEL rules grow past what's readable in a marker, at which point a validating webhook with real tests is the better home.
