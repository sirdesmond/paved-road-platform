# 08 — Progressive delivery with Argo Rollouts

**Mode:** faded
**Time:** two to three hours
**You'll end up with:** a workload that ships in stages, an automated check that decides whether to continue,
and a bad version that never reaches full traffic.

Assumes [07](./07-applicationsets.md) works.

---

## Files you'll create

Both at the repo root, both picked up automatically — the first by `platform-root`, the second by the
`team-environments` ApplicationSet.

```
paved-road-platform/
├── gitops/platform/argo-rollouts.yaml       # NEW: installs Argo Rollouts
└── environments/payments/
    ├── environment.yaml                     (already exists — must not be empty)
    └── demo-app.yaml                        # NEW: Rollout + AnalysisTemplate + Service
```

The `Rollout` targets `namespace: payments-dev`, which your controller creates from the `Environment`. So
`environment.yaml` has to actually define one, and its `metadata.name` must be `payments-dev` — the
controller names the namespace after the Environment.

## Why this exists

Everything you've built so far makes deployment *correct*. None of it makes deployment *safe*.

A Deployment rollout replaces pods as fast as the readiness probe allows. Readiness only asks "is the process
up", which a broken build usually is — it starts fine and returns 500s, or serves the wrong data, or is
quietly ten times slower. By the time anyone notices, every pod is the new version and the old ReplicaSet is
scaled to zero.

Progressive delivery splits that into steps with a decision between them. The decision is the important part:
a canary with a human watching a dashboard is just a slow deploy with extra anxiety.

## The honest limitation of doing this locally

Proper canary needs **traffic shaping** — send 5% of requests to the new version — which needs a mesh or an
ingress controller that Argo Rollouts can drive (Istio, Gateway API, NGINX, ALB).

Without one, Rollouts does **replica-based canary**: it scales the canary to a proportion of pods and lets the
Service load-balance across everything. With 10 pods, 2 canary pods gets you roughly 20% of traffic, by
accident of round-robin rather than by design.

That's enough to learn the mechanism and to catch a version that's broken for everyone. It is *not* enough for
"1% of traffic for 30 minutes", and you should say so rather than imply otherwise. Wiring real traffic
shaping is Phase 3 of the roadmap, once Envoy Gateway is in.

## Step 1: install Argo Rollouts, via GitOps

`gitops/platform/argo-rollouts.yaml` — another entry in the directory `platform-root` already watches:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: argo-rollouts
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argo-rollouts
    targetRevision: "2.*"
  destination:
    server: https://kubernetes.default.svc
    namespace: argo-rollouts
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
```

Get the CLI plugin too — the rollout view is genuinely the best part of the tooling:

```bash
brew install argoproj/tap/kubectl-argo-rollouts
```

## Step 2: a workload to canary

You need something that serves traffic, so use a trivial one in an existing team namespace. A `Rollout` is a
drop-in replacement for a `Deployment` — same pod template, different `spec.strategy`.

Put this in `environments/payments/demo-app.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: demo
  namespace: payments-dev
spec:
  replicas: 5
  selector:
    matchLabels: {app: demo}
  template:
    metadata:
      labels: {app: demo}
    spec:
      containers:
        - name: app
          image: nginxdemos/hello:plain-text
          ports:
            - containerPort: 80
          resources:
            requests: {cpu: 10m, memory: 32Mi}
            limits: {cpu: 50m, memory: 64Mi}
  strategy:
    canary:
      steps:
        - setWeight: 20
        - pause: {duration: 30s}
        - setWeight: 60
        - pause: {duration: 30s}
        # TODO(you): add the analysis step. See below.
---
apiVersion: v1
kind: Service
metadata:
  name: demo
  namespace: payments-dev
spec:
  selector: {app: demo}
  ports:
    - port: 80
      targetPort: 80
```

Note the resource requests — the namespace has a quota from your controller, and a Rollout that exceeds it
will fail to scale in a way that looks like a Rollouts bug and isn't. That interaction is worth seeing.

## Step 3: the analysis (your turn)

Without this, you have a slow deploy. With it, you have a gate.

Write an `AnalysisTemplate` in the same namespace and reference it from a rollout step:

```yaml
  strategy:
    canary:
      steps:
        - setWeight: 20
        - analysis:
            templates:
              - templateName: canary-healthy
        - setWeight: 60
        # ...
```

Most examples use the Prometheus provider, which you don't have. Use the **job provider** instead: Argo
Rollouts runs a Kubernetes Job, and a zero exit code means the step passes.

TODOs:

- **Write the probe.** A Job that curls the canary a number of times and exits non-zero if too many requests fail. `curlimages/curl` with a small shell loop is plenty.
- **Decide what "healthy" means.** Any failure at all, or a threshold? Ten requests or two hundred? A threshold of zero failures sounds rigorous and will abort your rollouts on one unlucky connection reset.
- **Decide the failure policy.** `failureLimit`, and how many times analysis retries before it gives up.
- **Work out what the Job should target.** The Service load-balances across canary *and* stable pods, so probing it tests the mix, not the canary. Getting a canary-only endpoint is the thing traffic shaping would give you for free — think about what you can do without it, and be honest in a comment about what your probe is really measuring.

That last point is the real lesson of this example. Analysis is only as good as its ability to observe the
canary specifically.

## Step 4: break it deliberately

Ship a version that starts fine and serves errors — an image that doesn't exist gives you a crash rather than
a bad response, which tests a different thing. Something that returns 500s is the interesting case.

```bash
kubectl argo rollouts get rollout demo -n payments-dev --watch
```

You want to see it stop partway, the analysis fail, and the canary scale back to zero with stable untouched.

**A rollout that aborts is a success.** If nothing has ever aborted, you haven't tested the gate, you've tested
the happy path with extra steps.

---

## Checkpoint

```bash
kubectl argo rollouts get rollout demo -n payments-dev
kubectl argo rollouts status demo -n payments-dev
```

A good version:

```bash
# change the image tag in Git, commit, push
kubectl argo rollouts get rollout demo -n payments-dev --watch
```

Steps advance, analysis passes, stable moves to the new version.

A bad version:

```bash
# push a broken image
kubectl argo rollouts get rollout demo -n payments-dev --watch
kubectl argo rollouts status demo -n payments-dev     # Degraded
kubectl get pods -n payments-dev                       # canary gone, stable serving
```

Then confirm the important property: **traffic never fully moved.** The stable ReplicaSet stayed up
throughout, which is what separates this from a Deployment that rolls back after everyone has already seen
the errors.

## If it went wrong

| What you see | Usually means |
|---|---|
| Rollout stuck at step 1 | A `pause` with no `duration` waits for a human. Resume with `kubectl argo rollouts promote demo -n payments-dev`. |
| Canary pods Pending | Your quota. The namespace ceiling has to cover canary *plus* stable during a rollout — a real capacity consideration, not a bug. |
| Analysis always fails | Job can't resolve the Service, or its ServiceAccount can't run in that namespace. Check the Job's pod logs, not the Rollout status. |
| Argo CD shows the app `Progressing` forever | A paused rollout is genuinely in progress. Argo CD understands Rollout health, so this is accurate rather than broken. |
| Argo CD reverts the rollout mid-flight | `selfHeal` fighting the Rollouts controller over the same object. Worth understanding before you work around it — see reflection 2. |

## Reflection

1. Your analysis probes the Service, which load-balances across canary and stable pods. If the canary is completely broken, what does a 20% canary do to your measured success rate — and would your threshold catch it?
2. Argo CD's `selfHeal` reverts drift; the Rollouts controller deliberately changes replica counts mid-rollout. Why don't these fight, and what would you have to configure if they did?
3. Right now a team writes their own `Rollout`. Should they? What would it mean for the platform to *provide* progressive delivery as a property of an `Environment` rather than something each team assembles?

That third question is the Phase 3 conversation, and the honest answer decides whether this platform is a
paved road or a pile of examples.

Next: Part 3 — `platform-api`, the CLI, and the cloud foundations.
