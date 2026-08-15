# Interview prep — mapping this project to a Staff platform role

> Working notes, not part of the project's public story. The repo itself reads as a genuine engineering
> lab; this file is where you connect it to a specific job spec.

## Responsibility → where it shows up

| What the role asks for | Where this project demonstrates it | Honest status |
|---|---|---|
| Turn ambiguous infra problems into proposals, drive via RFCs | [RFC-0001](./rfc/0001-self-service-environments.md), ADR-0001/0002 | ✅ written — strongest artifact |
| Self-service capabilities + platform APIs **in Go** | `platform-api`, `environment-controller`, `platformctl` (Phase 2) | 🔨 `environment-controller` built: CRD, reconciler, ownership, derived status. `platform-api` and CLI still to do |
| Delivery standards: Terraform, GitOps/Argo CD, progressive rollout, testing, CD | Phases 0–1 | ⚠️ partially proven (GitOps loop works in sibling repo) |
| Multi-tenant EKS: reliability, security, scale, cost | Phase 0 + tenancy model in ARCHITECTURE §3 | ⚠️ designed |
| Envoy Gateway ingress, traffic routing | Phase 3, [ADR-0002](./adr/0002-envoy-gateway-for-ingress.md) | ⚠️ decided, not built |
| Multi-region, cross-account connectivity | Phase 0 + ARCHITECTURE §5 | ⚠️ designed |
| SLOs, alerting, incident follow-up (Grafana Cloud) | Phase 4, runbook + postmortem template | ⚠️ templates exist |
| AI-assisted ops, with healthy skepticism | Phase 5 + ARCHITECTURE §7 (explicit boundary table) | ✅ position articulated |
| On-call ownership and improving on-call health | Runbooks, postmortem template, alert-quality review in Phase 4 | ✅ practice defined |
| Cross-team alignment, clear written communication | The RFC/ADR set is itself the evidence | ✅ |

## The three things to build first (biggest signal per hour)

1. **`environment-controller` in Go.** A real controller-runtime reconciler for the `Environment` CRD. This is the single most load-bearing artifact — it proves Go, Kubernetes internals, and platform API design at once.
2. **`platform-api` + PR flow.** Shows the judgment call that self-service doesn't mean abandoning audit/revert.
3. **The Argo CD ApplicationSet fan-out** proving "a new region is a variable change." That's their stated roadmap goal, demonstrated.

## Talking points worth rehearsing

- **Why PR-based self-service over direct provisioning.** Audit + diff + revert are what make it safe to hand out; auto-merge for non-prod removes the friction. (RFC-0001, "Why PR-based.")
- **The Terraform/controller boundary**, with the one-sentence litmus test: *if a change is triggered by a team's request rather than a platform decision, it doesn't belong in Terraform.* (ADR-0001.)
- **How you'd close the old path.** The paved road must be measurably faster *before* you restrict direct access — rollout Phase D. Getting this backwards is how platform teams become resented.
- **Where AI-assisted ops earns its place**: context assembly and recommendations, never unattended production mutation, everything logged and attributable. Have the boundary table ready.
- **Why the policy engine isn't Kyverno by default** ([ADR-0004](./adr/0004-policy-enforcement-layers.md)). Good current-knowledge signal: ValidatingAdmissionPolicy went GA in 1.30 and MutatingAdmissionPolicy in 1.36, so native CEL policy handles the structural rules in-process with no webhook to run or fail open. Kyverno comes in only for image signature verification, because CEL can't make external calls. Then the design point underneath it: admission is the *last* place to catch a problem, so most of the guardrail work lives at request time in the Go API where the error message can actually help someone.
- **Adoption as the real metric.** "If teams don't use the paved road, the paved road is wrong" — and how you'd instrument it from day one.
- **Why no portal and no Crossplane in v1** ([ADR-0003](./adr/0003-no-portal-or-crossplane-in-v1.md)). Strong Staff answer: a portal decorates a provisioning bottleneck rather than removing it, and environment provisioning is procedural logic that belongs in testable Go. Then name the revisit triggers — that's what separates "I scoped this" from "I didn't get to it." If asked whether you know them, point at the sibling repo where you ran Crossplane v2 and debugged real CRD failures.
- **The best debugging story in this repo**: an admission policy deadlocked an object with a finalizer ([runbook 0004](./runbooks/0004-admission-policy-deadlocks-finalizer.md)). Deleting needed the finalizer removed, removing it was an `UPDATE`, and the policy denied every update because the object still violated the rule. Two guardrails you built, each correct alone, combining into an object that could never be deleted — and audit mode couldn't reveal it, because in Audit the update succeeds. The fix is a transition rule on `oldObject` that permits unchanged violations. The transferable point: **enforcing a policy changes the meaning of every future write to a matching object, not just writes to the offending field.**
- **A second debugging story.** The Kyverno CRD annotation-size incident from the sibling repo: symptom (controller crashloop), false leads (assumed a missing chart), the tell (only the largest CRDs missing → size limit, not rendering), fix (server-side apply), prevention (default SSA for CRD-bearing charts). Staff interviews want the reasoning, including the wrong turns.

## Controller questions, worked through

From the reflection section of [worked example 02](./worked-examples/02-reconciler.md). These come up in
almost every platform interview that touches Kubernetes, usually phrased differently.

### The controller died halfway through creating three objects. What happens?

The process restarts, the manager's informers list every `Environment` on startup and enqueue them, and
reconcile runs from the top. The namespace already exists, so `CreateOrUpdate` fetches it, the mutate function
produces no diff, and nothing is written. Same for the quota. The missing network policy gets created.

It's fine because every step is idempotent and reconcile is a convergence function, not a transaction. There's
no partial state to clean up and nothing to roll back to, because "correct" is defined by the spec rather than
by how far through a sequence you got. It's also exactly why a plain `Create` would be wrong — it fails on the
second pass with `AlreadyExists`.

Follow-up worth volunteering: between the crash and the next reconcile, status is stale. That's what
`observedGeneration` is for — it distinguishes "this status describes the current spec" from "this status
predates your change".

### Someone hand-edits the quota to give themselves more CPU. How long does it last?

About a second.

`Owns(&corev1.ResourceQuota{})` sets up a watch with an owner-mapping handler: any event on a quota maps back
to the `Environment` in its controller ownerReference, that Environment is enqueued, and reconcile overwrites
`spec.hard`. It only works because the owner reference is set — without it the event maps to nothing and the
edit sticks.

Two bits of nuance that show you've actually run one of these. Anything the reconciler *doesn't* set (an
annotation, say) survives, because the mutate function only writes fields it owns. And even with no watch, the
manager's resync — 10 hours by default — would eventually catch it.

The operational point: self-healing means someone's change vanishes with no explanation, which is baffling if
they don't know the platform works this way. An argument for restricting direct cluster access *after* the
paved road is faster, not before.

### Why doesn't a namespace stuck in Terminating mean your controller is broken?

Because that phase isn't yours. Namespace deletion is two-phase, and the second phase belongs to the namespace
controller in kube-controller-manager: it enumerates and deletes every namespaced resource inside before
removing the `kubernetes` finalizer.

Stuck almost always means one of three things: a resource inside has a finalizer nobody is removing, an
aggregated APIService is unavailable so the namespace controller can't enumerate that resource type (the
classic metrics-server case), or a webhook is rejecting the deletes. Diagnose with
`kubectl get ns X -o jsonpath='{.status.conditions}'`, which names the failing group directly.

Your involvement ended when garbage collection issued the delete. The general instinct is the point: half of
platform on-call is telling "our thing is broken" apart from "Kubernetes is doing something slow and correct".

### Controller vs admission policy vs request-time validation — what does each catch?

They act at three different *moments*, which is why none replaces another. See
[ADR-0004](./adr/0004-policy-enforcement-layers.md).

**Request time** is the only layer with context the cluster doesn't have: budget across all of a team's
environments, current capacity, whether a request this size needs a human. It also owns the error message —
"your team's budget is 32 CPUs and you've asked for 64" is something a person can act on. Weakness: it only
sees requests that come through it.

**Admission** catches everything regardless of path — CI, a stray `kubectl`, another controller, or your own
controller with a bug in it. Unbypassable by anyone with cluster access, which is its whole value. In exchange
it's structurally limited (CEL can't call out or read other resources) and its messages are generic.

**The controller** isn't a gate at all — it's the only one that *repairs*. Admission can reject a bad write but
can't fix an object that's already wrong; request-time validation never sees the object again after creation.
Continuous convergence is the property only the controller has.

Compressed, and worth memorising in this form: **request-time knows why, admission catches everyone, the
controller fixes it later.**

## GitOps questions, worked through

From the reflection section of [worked example 06](./worked-examples/06-argocd-app-of-apps.md).

### When is `selfHeal: true` wrong?

Two cases, and they're different problems.

**Another controller legitimately owns a field.** An HPA scales a Deployment to 10, Argo CD sees Git says 2
and reverts it. Two controllers with opposite opinions about one field, and the HPA loses every sync
interval. The fix isn't disabling self-heal, it's `ignoreDifferences` on `/spec/replicas` — declaring that Git
owns the shape of the Deployment but not that number. Same for anything an admission webhook mutates.

**An incident.** Self-heal reverts your emergency fix while you're still watching, quietly, and the symptom
returns minutes after you thought you'd fixed it.

The emergency path has to exist before it's needed. Best: commit to Git with an expedited review — still
fastest with a short sync interval, and it leaves a record. Otherwise disable auto-sync on the single app
(`argocd app set <app> --sync-policy none`), make the change, and open a ticket to reconcile Git with reality
*before* re-enabling. Skip that last step and the next sync silently undoes your fix hours later.

The line worth saying out loud: **if the only way to fix production is to fight your own tooling, you'll do it
badly at 3am.** The escape hatch belongs in a runbook, written while calm.

### You delete an app's manifest from Git and `prune: true` is set. What happens?

Something much worse than intended, if the Application bundles CRDs with the workload.

`platform-root` prunes the child Application. Whether that cascades depends on the
`resources-finalizer.argocd.argoproj.io` finalizer: with it, everything the Application managed is deleted;
without it, resources are orphaned and keep running.

If it cascades and the app includes `config/crd`, the CRD goes. Deleting a CRD deletes every custom resource
of that type. Each one carries the controller's finalizer, and the controller's Deployment was deleted in the
same sweep — so nothing can remove those finalizers. The CRs hang in `Terminating`, the CRD hangs behind them,
and the namespaces they owned are never collected.

You removed a deployment manifest, lost the data, and wedged the cluster on the way out.

**The lesson: prune's blast radius is whatever happens to be in the Application.** CRDs don't belong in the
same one as the workload that serves them. Split them out with pruning off, or annotate:

```yaml
    argocd.argoproj.io/sync-options: Prune=false,Delete=false
```

Worth breaking on a throwaway cluster once. Far more memorable than reading about it.

### Argo CD is installed imperatively. Where does the bootstrap chain stop?

Something has to create the cluster and install Argo CD, and that something can't be Argo CD. The irreducible
root is: a cluster, a controller that can pull from Git, and credentials to reach Git. Everything above it
converges — Argo CD can even manage its own manifests after the first install, so upgrades become GitOps'd,
but that first install is always a human running a command.

Recovery is therefore: create cluster → install Argo CD → apply the root app → wait.

That's only true for what's *in* Git, and three things usually aren't:

- **Secrets** — which is why External Secrets (or equivalent) is a precondition for claiming you can rebuild.
- **Stateful data** in volumes, which GitOps never recovers.
- **Anything created imperatively that nobody wrote down** — the real killer, discovered only during the rebuild.

A worked example of getting this wrong, from this repo: the Argo CD install points at `stable`, a moving
target. Rebuild in six months and you get a different version. Fine for a lab, disqualifying for anything
described as reproducible. Pin it.

### One region is unreachable. What do the others do?

Nothing — they're unaffected. Each generated Application is an independent object with its own sync and health
state, so a destination failure is contained to one node in the tree. (Demonstrated accidentally, by pointing
a list-generator element at a cluster that didn't exist.)

The unreachable one goes `Unknown` for sync, because Argo CD can't compare against a cluster it can't read,
and its sync operations fail with backoff.

**The ApplicationSet itself stays healthy**, and the reason is the interesting part: its status reports on
*generation*, not on the health of what it generated. The cluster generator reads cluster Secrets from the
Argo CD namespace — it never contacts the clusters. Generation is decoupled from reachability. The dependency
that *would* break it is Git: if the repo is unreachable, a git generator can't enumerate and the
ApplicationSet errors.

Operational subtlety worth volunteering: Argo CD won't prune what it can't see, so an outage causes no
deletions. But deregistering the broken cluster during the incident makes the generator stop producing that
Application, Argo CD tries to clean up, fails to reach the cluster, and leaves the resources orphaned and
unmanaged when it returns. **Don't deregister clusters during incidents.**

### A team commits a request for 200 CPUs. What stops them?

Today, nothing — and being able to say that precisely is the point.

**Request-time validation doesn't apply.** They edited a file in Git; `platform-api` was never involved. That's
the documented weakness of that layer, and in this flow it isn't theoretical — Git *is* the path everyone uses.

**Admission should catch it and doesn't.** There are bounds on `pods` but none on CPU or memory, so the API
server accepts it.

**The controller isn't a gate.** It faithfully creates a quota nobody can satisfy, and the failure surfaces
much later as pods stuck Pending — far in time and place from whoever caused it.

The finding this produced is the good bit: [ADR-0004](./adr/0004-policy-enforcement-layers.md) assumed
requests arrive through the API, but the GitOps flow bypasses it entirely. The fix is that **CI on the pull
request becomes the request-time layer** — same rules, run before merge, reported where the developer already
is. Admission stays as the backstop that can't be skipped. The ADR now carries that as an amendment.

### Adding a directory grants an environment. What does that make PR review?

An authorization control, not a code-quality step. **Merge permission is provisioning permission.**

Three consequences: `CODEOWNERS` has to scope teams to their own directories, or one team can edit another's.
Platform config needs stricter ownership than tenant requests, and eventually a separate repo, so that being
able to *request* an environment doesn't imply being able to change the platform that grants them. And Git
history becomes the provisioning audit log — who approved what, when — which is better evidence than most
ticket systems produce.

Branch protection on that path is a security setting. Worth saying in exactly those words.

## Progressive delivery questions, worked through

From [worked example 08](./worked-examples/08-progressive-delivery.md).

### Your probe hits the Service, which balances across canary and stable. Does it catch a broken canary?

At 20% with 5 replicas there's 1 canary pod, so ~4 failures in 20 requests — over a 2-failure threshold, so
yes. (The analysis step actually runs at 60%, where it's ~12/20.)

The uncomfortable part is what happens when you make the canary *safer*. Aggregate error rate scales with
canary weight, so a **100%-broken canary at 5% weight produces a 5% aggregate error rate** — statistically
indistinguishable from noise, and with 20 requests you might see zero failures by chance.

**The smaller and safer the canary, the more invisible its failure becomes to aggregate measurement.**

Two consequences worth stating:

- Replica-based canary can't go below `1/replicas`. Five pods means 20% is the floor; a 5% canary needs 20 replicas. Traffic shaping decouples weight from replica count.
- Real canary analysis doesn't measure the aggregate at all. It queries canary and stable metrics *separately*, filtered on the pod-template-hash label, and compares them. That — not the traffic splitting itself — is the actual reason a mesh and per-version metrics matter.

### Argo CD's selfHeal reverts drift. Rollouts changes replica counts mid-rollout. Why don't they fight?

Because the ownership boundary is clean. Argo CD's desired state is the `Rollout` object in Git. The Rollouts
controller manipulates ReplicaSets and pods *beneath* it — not in Git, not created by Argo CD, and carrying
owner references marking them as controller-managed children. Nothing Rollouts touches is something Argo CD
has an opinion about.

They fight when a controller mutates the Rollout's **own spec**. Three realistic cases:

- **An HPA scaling `spec.replicas`** — the classic. `ignoreDifferences` on `/spec/replicas`.
- **`kubectl argo rollouts pause`**, which sets `spec.paused: true`. Argo CD reverts it and your rollout un-pauses itself. Needs `ignoreDifferences` on `/spec/paused`.
- **`kubectl argo rollouts set image`**, which writes the image into the spec and gets reverted to whatever Git says. Arguably correct — imperative promotion is fighting GitOps — but it means the CLI's most convenient command is one a GitOps platform shouldn't use.

### Should teams write their own Rollout?

No, and the manifest is the evidence: ~80 lines, most of it delivery *policy* — step weights, pause
durations, failure thresholds, the definition of healthy. Only a few lines are the team's application.

Left to teams, every squad invents its own rollout policy, quality varies with whoever wrote it, thresholds
get loosened after a flaky Friday, and the platform can't state anything true about how changes reach
production. Hand-built environments again, one layer up.

**The boundary: the platform owns the policy, teams own the payload.** Three moves that don't require the
platform to own application specs:

- A **`ClusterAnalysisTemplate`** with vetted thresholds, so teams reference `templateName: standard-canary` rather than writing probe logic. One place to fix when the thresholds prove wrong.
- The strategy as a **kustomize base or golden-path template** — teams supply image, name, resources; steps come from the platform.
- An **admission rule** that prod-tier namespaces accept only `Rollout` (not bare `Deployment`) referencing an approved analysis template. That makes the paved road the only road to production without owning what teams deploy.

The cost is the same as the tier-defaults table: changing the standard steps becomes a fleet-wide change.
Which is correct — that's a decision the platform should make deliberately rather than fifty teams making it
by accident.

## Admission policy questions, worked through

From [worked example 09](./worked-examples/09-admission-policy.md).

### What's the blast radius of a CEL typo under `failurePolicy: Fail`?

Everything matching `matchConstraints` and the binding's scope, immediately and totally — and since policies
ship via GitOps, a bad push reaches every targeted cluster within a sync interval.

But the webhook comparison favours native policy in a way worth articulating. A webhook with the same failure
policy has the *same* blast radius plus a much larger set of causes: evicted pod, drained node, expired
certificate, network partition, timeout. Most webhook outages have nothing to do with policy logic. **VAP runs
in-process, so it can only fail because you wrote something wrong** — a strictly smaller failure surface, and
a better argument for native policy than operational convenience is.

Two mitigations:

- The API server **type-checks CEL against the schema at policy creation**, catching a whole class of typos before the policy ever runs: `kubectl get validatingadmissionpolicy X -o jsonpath='{.status.typeChecking}'`. What it can't catch is a logically wrong expression — right types, wrong meaning. That's what Audit mode and fixtures are for.
- Never let a policy match `validatingadmissionpolicies` or their bindings. That's the one genuine way to lock yourself out, because the escape hatch becomes unreachable.

### You enforce, and fifty environments already violate the rule.

They keep running, every update to them is rejected, controllers writing their status hot-loop on errors, and
any with a finalizer become **undeletable** (see [runbook 0004](./runbooks/0004-admission-policy-deadlocks-finalizer.md)).
A policy meant to prevent bad new things has broken operations on existing ones.

What should happen: survey first, then choose deliberately between fixing them, grandfathering with an
`oldObject` transition rule, or accepting the breakage. Usually the transition rule, because it separates two
things that shouldn't be conflated — **enforcement stops the bleeding; remediation is a tracked workstream
with its own timeline.** Big-bang enforcement merges them and breaks someone's Friday.

The audit phase should produce the burn-down list, not just confidence that the rule works.

### The same ceilings exist in `platform-api` and in the policy ConfigMap. Which wins?

The ConfigMap, because it can't be bypassed. The API's table isn't a second source of truth — it's a *preview*
of the rule, existing for a better message delivered earlier. Same relationship as client-side form validation
to server-side.

Drift matters **asymmetrically**, which is the useful part:

- API **stricter** than policy → teams refused something that would have been allowed. Annoying, safe.
- API **laxer** than policy → the API opens a PR, it merges, and Argo CD can't apply it. The failure moves from request-time to sync-time, far from the developer, leaving a merged commit that can't be applied.

A single source is achievable: have `platform-api` read the same ConfigMap at startup instead of hardcoding a
table. Failing that, a CI test asserting they match costs nothing. What doesn't work is relying on someone
remembering to update both.

### Why can't the policy enforce "this team's total budget across all environments"?

CEL in admission is deliberately sandboxed — no cluster reads, no external calls, no state. Every evaluation
sits in the hot path of every write and must be fast, deterministic and side-effect free. Allowing
cross-resource queries would make admission a distributed query engine on the critical path, and it'd still be
wrong: two simultaneous requests would both see the pre-request total.

Three homes, with different properties. `platform-api` can compute it (cluster access, good errors) but is
bypassable. A webhook could, buying back availability risk. Or — the interesting option — **a controller
materialises it**: it watches Environments, maintains a per-team object with `status.usedCPU`, and the
policy's `paramRef` selects that object. Admission then does a local comparison against a precomputed value.
You've turned a cross-resource query into a param lookup.

It's best-effort rather than transactional, since the materialised total lags. That's acceptable because the
controller reconciles the truth afterwards. **Admission prevents the obvious cases; reconciliation catches the
races.**

## Gaps to be honest about

- No production-scale operational experience *with this specific stack* — compensate by being precise about what you built vs. designed. Never imply the designed parts are running.
- Envoy Gateway and Grafana Cloud are decided-but-unbuilt here; say so plainly and describe how you'd validate the choices.
- The multi-region story is architectural until a second region actually exists. "Adding a region should touch a variable file and a generator" is a claim you should be ready to either demonstrate or label as a design intent.
