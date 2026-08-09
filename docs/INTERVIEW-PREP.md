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
- **A real debugging story.** The Kyverno CRD annotation-size incident from the sibling repo: symptom (controller crashloop), false leads (assumed a missing chart), the tell (only the largest CRDs missing → size limit, not rendering), fix (server-side apply), prevention (default SSA for CRD-bearing charts). Staff interviews want the reasoning, including the wrong turns.

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

## Gaps to be honest about

- No production-scale operational experience *with this specific stack* — compensate by being precise about what you built vs. designed. Never imply the designed parts are running.
- Envoy Gateway and Grafana Cloud are decided-but-unbuilt here; say so plainly and describe how you'd validate the choices.
- The multi-region story is architectural until a second region actually exists. "Adding a region should touch a variable file and a generator" is a claim you should be ready to either demonstrate or label as a design intent.
