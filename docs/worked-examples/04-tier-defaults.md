# 04 — Tier defaults and validation

**Mode:** solo — a goal, the decisions you have to make, and a checklist. Hints at the bottom if you want them.
**Time:** two to four hours, most of it deciding rather than typing.

Assumes [03](./03-finalizers.md) works.

---

## The problem

Your `tier` field claims to drive defaults. It doesn't. A dev environment and a production one both get
4 CPUs, 8Gi and 20 pods, because those are schema defaults and schema defaults are constants.

There's also a lie in your API. You can set `ttl: 24h` on a prod environment and it's silently ignored —
`effectiveTTL` returns nil regardless. Accepting input and quietly discarding it is worse than rejecting it,
because the user reasonably believes it took effect.

So there are two jobs here, and they need different tools:

- **Defaulting**: tier should decide the resource ceiling when the team doesn't specify one.
- **Validation**: combinations that don't make sense should be rejected at admission, with a message that says what to do instead.

## What done looks like

- A dev environment with no `resources` gets a small ceiling. A prod one gets a large one. Neither team wrote a number.
- A team can still override, and their value wins.
- `ttl` on a prod environment is rejected at admission, with a message explaining that production doesn't expire.
- Changing `tier` on an existing environment is rejected. (Think about why before you implement it — see the decisions below.)
- Every rejection message tells the user what to do, not just what's wrong.
- Existing environments still reconcile. You haven't broken `search-dev`.

## The decisions you have to make

This is the actual work. The code is short once you've decided.

**1. Where do tier defaults belong?**

Three places, and they're genuinely different, not just stylistic:

- **A defaulting (mutating) webhook.** Runs at admission, so the stored object is complete. `kubectl get env -o yaml` shows the team exactly what they got, and everything downstream reads one consistent spec. Cost: a webhook is a service, with certificates, availability, and a failure policy. If it's down, do you block all environment creation, or let unvalidated objects through? Both answers are bad in different ways.
- **The controller.** No new infrastructure. But the spec then doesn't reflect reality: the object says nothing about resources while the quota says 32 CPUs. Anyone reading the spec to understand the system gets a wrong answer, and that includes your future `platform-api`.
- **`platform-api` at request time.** Fills them in before the object exists. Good UX, and it's where budget context lives — but it only applies to requests that came through the API, so anything applied directly skips it.

There's a defensible case for each. Pick one, and write the ADR *before* you write the code, because the reasoning is the part you'll be asked about.

**2. Should `tier` be immutable?**

Changing tier changes the quota, the defaults, and whether the thing expires. Promoting staging to prod by editing one field is either a lovely feature or a way to accidentally give a team a production-sized quota with no review.

Consider what "promote to prod" *should* look like in your platform. If it's a new environment plus a migration, tier is immutable. If it's a legitimate in-place operation, it isn't, and something else has to gate it.

**3. Validation via CEL or via a webhook?**

CEL rules (`+kubebuilder:validation:XValidation`) live in the CRD schema and are enforced by the API server. No webhook, no certificates, nothing to run, and they can't be bypassed. They also can't call out, can't read other objects, and can't produce dynamic messages.

A validating webhook can do all of that, and costs you a service to operate.

For everything in this exercise, CEL is enough. Reach for the webhook only when you hit something CEL genuinely can't express — which is the same argument as [ADR-0004](../adr/0004-policy-enforcement-layers.md), applied one level down.

**A warning worth having before you commit to a webhook:** running one locally against kind is awkward. The
API server has to reach your laptop over TLS with a cert it trusts, which means either deploying into the
cluster properly with cert-manager, or a tunnel. If you go the webhook route, budget an evening for the certs
alone. That difficulty is itself an argument for CEL, and worth mentioning in your ADR.

## Checkpoint

```bash
# defaults differ by tier
kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata: {name: tiny-dev}
spec:
  owner: {team: search, contact: "#team-search"}
  tier: dev
EOF

kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata: {name: big-prod}
spec:
  owner: {team: payments, contact: "#team-payments"}
  tier: prod
EOF

kubectl get env tiny-dev -o jsonpath='{.spec.resources}{"\n"}'
kubectl get env big-prod -o jsonpath='{.spec.resources}{"\n"}'
kubectl get resourcequota quota -n big-prod -o jsonpath='{.spec.hard}{"\n"}'
```

Different ceilings, and the quota matches whichever source of truth your design says it should.

```bash
# explicit values still win
kubectl patch env tiny-dev --type=merge -p '{"spec":{"resources":{"cpu":"1"}}}'
kubectl get env tiny-dev -o jsonpath='{.spec.resources.cpu}{"\n"}'   # 1

# ttl on prod is rejected
kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata: {name: bad-prod}
spec:
  owner: {team: payments, contact: "#team-payments"}
  tier: prod
  ttl: 24h
EOF
```

That last one must fail. **Read the error as though you were the developer hitting it.** If it doesn't tell
them what to do instead, the message isn't finished — this is the developer experience of your platform, and
it's the part people actually judge.

```bash
# tier is immutable (if you decided it should be)
kubectl patch env tiny-dev --type=merge -p '{"spec":{"tier":"prod"}}'

# nothing existing broke
kubectl get env
```

## Hints

Only if you're stuck.

**CEL rules go on the type they apply to.** Placed on `EnvironmentSpec`, `self` is the spec, so you can write
rules that reference several fields at once — which is exactly what you need for "prod must not have a ttl".
`has()` tests for the presence of an optional field.

**Immutability uses transition rules**, which compare against `oldSelf`. They only run on update, and only on
fields present in both objects. Kubebuilder exposes these through the same `XValidation` marker.

**Messages are part of the rule.** The marker takes a `message`, and it's the only thing the user sees. "Tier
is immutable" is worse than telling them to create a new environment and migrate.

**If you go the webhook route**, `kubebuilder create webhook --group platform --version v1alpha1 --kind
Environment --defaulting --programmatic-validation` scaffolds it. Read what it generates for the failure
policy before you deploy it — the default is stricter than people expect.

**Whatever you choose for defaults, keep one source of truth.** If the webhook fills in resources, the
controller should read them, not recompute them. Two places that both know the dev ceiling is 2 CPUs will
disagree eventually, and the bug will be reported as "the quota is wrong sometimes".

## Before you move on

Write the ADR for decision 1. Three viable options, one chosen, with the failure mode of each named. That's
the most interview-relevant artifact in this whole example — more than the code.

Worked versions: [ADR-0006](../adr/0006-tier-defaults-in-the-controller.md) for where defaults live, and
[ADR-0007](../adr/0007-tier-is-immutable.md) for immutability. Write yours first. The thing to notice in 0006
is that the webhook alternative has a genuine advantage the chosen option doesn't — an ADR that can't say
what it gave up isn't finished.

Also update `effectiveTTL`: if a prod TTL is now rejected at admission, the nil-return branch is defending
against something that can't happen. Decide whether to keep it as belt and braces (and say so in a comment) or
remove it.

## Reflection

1. Your defaulting webhook is down. Should environment creation fail, or should objects be admitted without defaults? What does each choice break, and which is easier to explain afterwards?
2. You change the dev default from 2 CPUs to 4. What happens to the fifty dev environments that already exist? Should it?
3. CEL can't read other objects, so it can't enforce "this team's environments total under 64 CPUs". Where does that rule have to live, and what does that tell you about what admission control is fundamentally for?

Next: [05 — tests with envtest](./05-tests.md), including a reconcile that fails halfway.
