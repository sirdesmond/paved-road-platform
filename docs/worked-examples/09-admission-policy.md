# 09 — Admission policy: audit, then enforce

**Mode:** faded
**Time:** two to three hours
**You'll end up with:** native CEL policies closing the gap the ADR-0004 amendment named, rolled out the way
policy should be — observing first, blocking second.

Assumes Part 1. Your cluster needs Kubernetes 1.30+ for ValidatingAdmissionPolicy and 1.36+ for
MutatingAdmissionPolicy — you're on 1.36, so both are GA and there's nothing to install.

---

## Files you'll create

Policies are cluster resources, so they're deployed like everything else — by Argo CD, from Git. But
`gitops/platform/` holds Argo CD **Applications**, not raw manifests, so the policies need their own
directory and an Application pointing at it.

Four new files, all at the repo root:

```
paved-road-platform/
├── gitops/platform/policies.yaml                    # NEW: the Application
└── policies/                                        # NEW directory: the actual manifests
    ├── tier-limits-configmap.yaml                   # NEW: the ceilings (step 1)
    ├── environment-resource-bounds.yaml             # NEW: the policy (step 2)
    └── environment-resource-bounds-binding.yaml     # NEW: the binding (step 3)
```

`platform-root` already watches `gitops/platform/` recursively, so dropping `policies.yaml` there is all the
wiring needed — no bootstrap step, no `kubectl apply`.

**File: `gitops/platform/policies.yaml`**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: platform-policies
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/sirdesmond/paved-road-platform.git
    targetRevision: main
    path: policies
  destination:
    server: https://kubernetes.default.svc
    namespace: platform-system
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions:
      - CreateNamespace=true      # the ConfigMap needs platform-system to exist
```

`destination.namespace` only affects namespaced resources — the ConfigMap lands in `platform-system`, while
the policy and binding are cluster-scoped and ignore it.

Steps 6 and 7 add more files to `policies/`; the Application already syncs the whole directory, so they need
no extra wiring.

## The gap you're closing

[ADR-0004](../adr/0004-policy-enforcement-layers.md) put structural rules at admission because that layer
can't be bypassed. Then the amendment found a hole: `spec.resources.cpu` and `memory` have no bounds at all.
A team can commit a request for 200 CPUs and every layer accepts it — the API server has no rule, and the
controller isn't a gate, so it faithfully creates a quota nobody can satisfy. The failure surfaces days later
as pods stuck Pending.

This example closes that, and does it the way policy should be introduced: **nothing gets denied on day one.**

## Why audit first, always

A policy that denies on the day you write it will reject something you didn't anticipate, during someone
else's deploy, and you'll find out from an incident channel. CEL is expressive enough to be subtly wrong in
ways that only appear against real objects.

So: ship it in `Audit`, watch what it *would* have blocked, fix the rule or fix the offenders, then flip to
`Deny`. The mechanism makes this cheap — the action lives in the *binding*, not the policy, so promoting from
audit to enforce is a one-line change with no rewrite.

That sequencing is worth being able to describe in an interview. It's the difference between someone who has
written a policy and someone who has rolled one out.

## Step 1: parameters, not hardcoded numbers

Put the ceilings in a ConfigMap so the rule and the limits can change independently.

**File: `policies/tier-limits-configmap.yaml`**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: environment-tier-limits
  namespace: platform-system
data:
  dev-cpu: "8"
  staging-cpu: "16"
  prod-cpu: "64"
```

Same numbers as `platform-api`'s validation table, which is deliberate — see reflection 3.

## Step 2: the policy (worked)

**File: `policies/environment-resource-bounds.yaml`**

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: environment-resource-bounds
spec:
  failurePolicy: Fail
  paramKind:
    apiVersion: v1
    kind: ConfigMap
  matchConstraints:
    resourceRules:
      - apiGroups: ["platform.internal"]
        apiVersions: ["v1alpha1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["environments"]
  validations:
    - expression: >
        !has(object.spec.resources) || !has(object.spec.resources.cpu) ||
        quantity(string(object.spec.resources.cpu))
          .isLessThan(quantity(params.data[object.spec.tier + '-cpu']))
      messageExpression: >
        'cpu ' + string(object.spec.resources.cpu) + ' exceeds the ' + object.spec.tier +
        ' ceiling of ' + params.data[object.spec.tier + '-cpu'] +
        '. Ask in #platform if you need it raised.'
      reason: Invalid
```

Three things doing real work here.

**The `has()` guards.** `resources` is optional and `cpu` inside it is optional. CEL evaluating a missing
field is an error, not a false — and an erroring expression under `failurePolicy: Fail` rejects the request.
Unguarded optional fields are the most common way a policy blocks things it was never meant to touch.

**`quantity()`** parses Kubernetes quantities properly, so `"500m"` compares correctly against `"8"`. String
comparison would tell you `"9"` is greater than `"64"`.

**`messageExpression` rather than `message`.** The caller gets the actual number and the actual ceiling. A
static "resources too large" makes the person guess, and you've just built the layer whose weakness is poor
error messages — no need to make it worse.

## Step 3: bind it in Audit

The binding is where the policy meets reality, and where the action lives.

**File: `policies/environment-resource-bounds-binding.yaml`**

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: environment-resource-bounds
spec:
  policyName: environment-resource-bounds
  validationActions: ["Audit", "Warn"]   # not Deny. Not yet.
  paramRef:
    name: environment-tier-limits
    namespace: platform-system
    parameterNotFoundAction: Deny
  matchResources: {}
```

`Warn` sends the message back to the client as a warning without rejecting, so `kubectl apply` prints it and
nobody is blocked. `Audit` records it in the audit log. Together they give you visibility from both ends —
the user sees it, and you can count it.

`parameterNotFoundAction: Deny` matters: if someone deletes the ConfigMap, the policy fails closed rather
than silently permitting everything. A guardrail that disappears when its config does isn't a guardrail.

## Step 3b: ship it

```bash
mkdir -p policies
# create the three files above, plus gitops/platform/policies.yaml

kubectl apply --dry-run=server -f policies/     # validate before pushing
git add policies gitops/platform/policies.yaml
git commit -m "Bound Environment resources per tier (audit mode)"
git push
```

Then confirm Argo CD picked it up:

```bash
kubectl -n argocd get app platform-policies
kubectl get validatingadmissionpolicy environment-resource-bounds
kubectl get validatingadmissionpolicybinding environment-resource-bounds
kubectl -n platform-system get cm environment-tier-limits
```

All four should exist. If `platform-policies` doesn't appear at all, `platform-root` hasn't refreshed —
`kubectl -n argocd annotate app platform-root argocd.argoproj.io/refresh=hard --overwrite`.

## Step 4: watch it observe

```bash
kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata:
  name: greedy-dev
spec:
  owner: {team: search, contact: "#team-search"}
  tier: dev
  resources: {cpu: "200"}
EOF
```

It should **succeed**, printing a warning. That's the whole point of this step: you've learned the rule fires
correctly without having blocked anyone.

Check what it caught across everything that already exists:

```bash
kubectl get env -o json | jq -r '.items[] | "\(.metadata.name) \(.spec.tier) \(.spec.resources.cpu // "-")"'
```

**Existing objects are not re-validated.** Admission only runs on writes, so anything already in the cluster
that violates the rule stays until someone touches it. Finding those before you enforce is the actual work of
this step.

## Step 5: enforce (your turn)

> **Read this before you flip.** A policy matching `UPDATE` judges the object, not the change — so an object
> that already violates the rule can't be updated *at all*, including by your own controller removing a
> finalizer. That's a deadlock: deleting needs an update, the update is denied, the object becomes immortal.
> Audit mode won't reveal it, because in Audit the update succeeds.
>
> See [runbook 0004](../runbooks/0004-admission-policy-deadlocks-finalizer.md) for the fix — a transition rule
> using `oldObject` that permits unchanged violations while still blocking new or worsening ones.


In `policies/environment-resource-bounds-binding.yaml`, change `validationActions` to `["Deny", "Audit"]`,
push, and confirm `greedy-dev` is now rejected on update.

Then answer the question the flip raises: what do you do about the violating objects already in the cluster?
They keep running, and the next innocuous edit to one of them now fails. Is that acceptable? Do you fix them
first, grandfather them with a label the policy excludes, or accept a surprise for whoever edits next?

There isn't a right answer, but there is a wrong one, which is not thinking about it until it happens.

## Step 6: the same guardrail for workloads (your turn)

The Environment policy protects the *request*. Nothing yet protects what teams actually run.

Two more files, same pattern: `policies/workload-baseline.yaml` and
`policies/workload-baseline-binding.yaml`. The Application already syncs the whole `policies/` directory, so
nothing else needs changing.

Write a policy applying to Deployments and Rollouts in team namespaces:

- Containers must set CPU and memory **limits** (the quota can't protect a namespace whose pods declare nothing).
- No `:latest` tags, and no image without a tag.
- Required `owner` label.

Things to work out:

- **Scoping.** Use `matchConstraints` with a `namespaceSelector` on the label your controller sets (`platform.internal/team`), so the policy applies to team namespaces and not to `kube-system`. Getting this wrong locks you out of your own cluster.
- **Iterating containers.** CEL: `object.spec.template.spec.containers.all(c, has(c.resources.limits))`.
- **Init and ephemeral containers** — do they need the same rule? Decide, don't skip by accident.
- Ship it in Audit and leave it there for a bit. This one *will* catch things.

## Step 7: a mutating policy (your turn)

MAP is GA on your cluster. Use it for a default that genuinely belongs at admission rather than in your
controller — a `platform.internal/managed` label, or a `securityContext` baseline on team workloads.

The judgment worth exercising: **don't** use it to duplicate the tier defaults. Those live in the controller
by [ADR-0006](../adr/0006-tier-defaults-in-the-controller.md), and two things defaulting the same field is a
bug waiting for a disagreement. Mutation is for things nothing else owns.

---

## Checkpoint

```bash
kubectl get validatingadmissionpolicy
kubectl get validatingadmissionpolicybinding

# in Audit: succeeds with a warning
kubectl apply -f hack/policy-fixtures/greedy-environment.yaml

# after flipping to Deny: rejected, with your message
kubectl apply -f hack/policy-fixtures/greedy-environment.yaml
```

Then the property that makes this layer worth having:

```bash
# a request that never touches platform-api and never opens a PR
kubectl apply -f hack/policy-fixtures/greedy-environment.yaml
```

Rejected anyway. That's the difference between this and the API-layer check — no path around it.

## If it went wrong

| What you see | Usually means |
|---|---|
| Everything rejected, including valid objects | An unguarded optional field. A CEL evaluation error under `failurePolicy: Fail` is a rejection. Add `has()` guards. |
| Policy exists but nothing happens | No binding, or `matchResources` doesn't match. The policy alone does nothing — the binding activates it. |
| `no such key` on params | Tier value not in the ConfigMap, or a typo in the key expression. Note this errors rather than returning false. |
| Can't create anything in `kube-system` | Missing `namespaceSelector`. Exclude system namespaces explicitly. |
| Works on create, not on update | `operations` missing `UPDATE`. |

## Reflection

1. `failurePolicy: Fail` means a broken CEL expression rejects every matching request. What's the blast radius of a typo, and how does that compare with a webhook being down?
2. You enforced the rule. Fifty environments already violate it. What happens to them, and what should?
3. The same ceilings now exist in `platform-api`'s validation table and in this ConfigMap. Which is authoritative? What breaks when they drift, and is one source of truth actually achievable across a Go service and a CEL policy?
4. This policy can't ask "does this team have budget across all their environments". Why not, and where does that rule have to live?

Question 3 is the one this example exists to make concrete — you'll have written the same guardrail twice, at
two layers, and be able to say exactly what each buys.

Next: [10 — `platform-api`](./10-platform-api.md).
