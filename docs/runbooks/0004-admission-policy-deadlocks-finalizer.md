# Runbook 0004 — An object can't be deleted: admission policy vs finalizer

**Symptom:** a custom resource sits in `Terminating` indefinitely. The controller is running, healthy, and
reconciling other objects fine. Its logs show:

```
ERROR Reconciler error {"Environment": {"name":"greedy-dev"},
  "error": "environments.platform.internal \"greedy-dev\" is forbidden:
   ValidatingAdmissionPolicy 'environment-resource-bounds' with binding
   'environment-resource-bounds' denied request: cpu 200 exceeds the dev ceiling of 8."}
```

## What's happening

A deadlock between two of your own guardrails.

1. The object has a finalizer, so deleting it requires the controller to remove that finalizer.
2. Removing a finalizer is an **`UPDATE`** on the object.
3. The admission policy matches `operations: ["CREATE", "UPDATE"]` and evaluates the object — which still
   violates the rule, because the offending field hasn't changed.
4. The update is denied. The finalizer stays. The object can never be deleted.

The controller retries forever, correctly, and gets denied every time.

**Why the audit phase didn't catch it:** in `Audit`/`Warn` the update succeeds. The deadlock only appears the
moment you flip to `Deny`, and only for objects that already violate the rule.

## Immediate unblock

Make the object compliant. The update is evaluated against the **new** value, so this is permitted:

```bash
kubectl patch env greedy-dev --type=merge -p '{"spec":{"resources":{"cpu":"4"}}}'
kubectl get env greedy-dev      # finalizer clears, object deletes
```

Two worse options, for when that isn't possible:

- **Flip the binding to `Audit`** briefly, let the deletion complete, flip back. Disables the guardrail cluster-wide while you do it.
- **Strip the finalizer by hand** (`kubectl patch ... '{"metadata":{"finalizers":[]}}'`). The object vanishes and whatever the cleanup would have done never happens — see [runbook 0003](./0003-argocd-cluster-add-address-mismatch.md) on why that leaks.

Prefer the patch. It fixes the cause rather than removing the mechanism.

## The proper fix: don't make it worse

Policies that match `UPDATE` should judge the *change*, not just the state. Allow an update when the offending
value is unchanged:

```yaml
  validations:
    - expression: >
        !has(object.spec.resources) || !has(object.spec.resources.cpu) ||
        quantity(string(object.spec.resources.cpu))
          .isLessThan(quantity(params.data[object.spec.tier + '-cpu'])) ||
        (oldObject != null &&
         has(oldObject.spec.resources) && has(oldObject.spec.resources.cpu) &&
         string(oldObject.spec.resources.cpu) == string(object.spec.resources.cpu))
```

`oldObject` is null on CREATE, so new violations are still rejected outright. On UPDATE, an unchanged bad
value passes — finalizers come off, status gets written, labels can be fixed — while raising the value
further is still blocked.

That's grandfathering expressed as a rule rather than as a hand-maintained exception list, and it's the main
reason `oldObject` exists.

## How to avoid it next time

**Before flipping any policy to `Deny`, survey what already violates it**:

```bash
kubectl get env -o json | jq -r '.items[] | "\(.metadata.name)\t\(.spec.resources.cpu // "-")"'
```

Then decide, deliberately: fix them, grandfather them with an `oldObject` transition rule, or accept that the
next write to each will fail. All three are defensible. Discovering it during an incident is not.

**Be especially careful when the resource has a finalizer.** Without one, a delete on a violating object
succeeds and you never notice. With one, deletion needs an update, and the policy turns "this object is
non-compliant" into "this object is immortal".

## The general lesson

**Enforcing a policy changes the meaning of every future write to a matching object, not just writes to the
offending field.** Finalizer removal, status updates, label fixes and ownership changes all become subject to
a rule about something else entirely.

Whenever a policy matches `UPDATE`, ask: what else updates this object, and what happens to those writers when
an object is already non-compliant? Controllers are usually the answer, and they retry silently and forever.
