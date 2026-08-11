# Policy fixtures

Test inputs for the admission policies in [`../../policies/`](../../policies/), used by
[worked example 09](../../docs/worked-examples/09-admission-policy.md).

**Deliberately not under `policies/`** — Argo CD syncs that path, and `greedy-environment.yaml` would be
applied as a real Environment. Keep test inputs outside anything a GitOps controller watches.

| Fixture | Tests | Expected |
|---|---|---|
| `greedy-environment.yaml` | CPU above the tier ceiling | Warn in Audit, **rejected** in Deny |
| `reasonable-environment.yaml` | Control case, under the ceiling | **Always accepted** |
| `bare-environment.yaml` | No `resources` block at all | **Always accepted** — if not, your `has()` guards are missing |
| `plain-deployment.yaml` | Mutating policy injects securityContext | Dry-run output contains fields the input didn't |
| `bad-deployment.yaml` | No limits, `:latest`, no name label | Three warnings in Audit, **rejected** in Deny |

## Running them

```bash
# audit phase: everything applies, some with warnings
kubectl apply -f hack/policy-fixtures/greedy-environment.yaml
kubectl apply -f hack/policy-fixtures/bad-deployment.yaml

# the two that must never be rejected
kubectl apply -f hack/policy-fixtures/reasonable-environment.yaml
kubectl apply -f hack/policy-fixtures/bare-environment.yaml

# mutation, without persisting anything
kubectl apply --dry-run=server -o yaml -f hack/policy-fixtures/plain-deployment.yaml \
  | yq '.spec.template.spec.securityContext'

# scoping check: same bad manifest outside a team namespace should be fine
sed 's/namespace: payments-dev/namespace: default/' hack/policy-fixtures/bad-deployment.yaml \
  | kubectl apply --dry-run=server -f -
```

## Cleanup

```bash
kubectl delete -f hack/policy-fixtures/greedy-environment.yaml --ignore-not-found
kubectl delete -f hack/policy-fixtures/reasonable-environment.yaml --ignore-not-found
kubectl delete -f hack/policy-fixtures/bare-environment.yaml --ignore-not-found
kubectl delete -f hack/policy-fixtures/bad-deployment.yaml --ignore-not-found
```

Note the Environments here create real namespaces via the controller, so clean them up rather than leaving
three orphans behind.

## Why keep these

A policy you've only seen accept things isn't tested. These fixtures are the difference between "the CEL
parsed" and "the rule does what I meant", and the two negative cases (`bare-environment`, and
`bad-deployment` in `default`) matter more than the positive ones — they're the failure modes that block
legitimate work or lock you out of your own cluster.
