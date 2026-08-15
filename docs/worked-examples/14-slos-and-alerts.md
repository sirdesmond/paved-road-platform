# 14 — SLOs, alerts, and a runbook that gets used

**Mode:** worked
**Time:** two to three hours
**You'll end up with:** metrics your controller emits about the thing users care about, alerts derived from
an SLO rather than from raw resource noise, and every alert linked to a runbook.

Assumes Part 1 and example 06. Doesn't need Terraform — this all runs on kind.

---

## Files you'll create

```
paved-road-platform/
├── environment-controller/
│   └── internal/controller/metrics.go        # NEW: the two metrics that matter
├── gitops/platform/
│   ├── prometheus.yaml                       # NEW: kube-prometheus-stack via Argo CD
│   └── platform-slos.yaml                    # NEW: recording + alerting rules
└── docs/
    ├── slo/environment-provisioning.md       # NEW: the SLO, written down
    └── runbooks/0005-provisioning-failing.md # NEW: what the alert links to
```

## The decision that shapes everything

**SLO the platform, not the workloads.**

The obvious instinct is to alert on cluster things — node memory, pod restarts, disk. Those matter, but
they're not what your users experience. A team experiences your platform as: *I asked for an environment and
it appeared, or it didn't.*

So the two SLIs worth having are **provisioning success rate** and **time to ready**. Everything else is
diagnosis, not alerting.

There's a second reason. If provisioning is unreliable, teams stop trusting the paved road and go back to
asking humans — which destroys the entire premise. **The platform's own reliability is its adoption
strategy**, which makes it the right thing to put an error budget on.

## Part A: instrument the controller

controller-runtime already exposes reconcile counts, errors and durations for free. Useful, but reconcile-level
— not environment-level. A reconcile can fail nine times and the environment still ends up fine, so
reconcile errors overstate user pain.

Two custom metrics say what users feel.

**File: `environment-controller/internal/controller/metrics.go`**

```go
package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// Failures by reason. The reason label is the same string that goes in the
	// Ready condition, so an alert and `kubectl describe` agree with each other.
	environmentFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "environment_reconcile_failures_total",
		Help: "Environment reconcile failures, by reason.",
	}, []string{"reason"})

	// Creation to Ready. This is the number a developer would recognise as
	// "how long did it take", which is what makes it worth an SLO.
	environmentTimeToReady = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "environment_time_to_ready_seconds",
		Help:    "Time from Environment creation to the Ready condition.",
		Buckets: []float64{5, 10, 20, 30, 60, 120, 300, 600},
	})
)

func init() {
	// controller-runtime's registry, so these appear on the manager's existing
	// /metrics endpoint. No second server to run or scrape.
	metrics.Registry.MustRegister(environmentFailures, environmentTimeToReady)
}
```

`metrics.go` only declares and registers. **The instrumentation goes inside the reconciler's existing methods
in `environment_controller.go`** — these are lines to add, not new functions. They reference `before` and
`env`, which only exist in that scope.

**In `failed()`**, right after `before := env.Status.DeepCopy()`:

```go
	// Count every failure, including repeats during backoff — the alert reads
	// a rate, and a problem that keeps happening should keep counting.
	environmentFailures.WithLabelValues(reason).Inc()
```

**In `ready()`**, just before `meta.SetStatusCondition(...)`:

```go
	// Observe only on the transition to Ready. Recording on every reconcile
	// would pile up samples for environments that have been ready for days,
	// and you'd end up measuring uptime while believing you were measuring
	// provisioning time.
	if !meta.IsStatusConditionTrue(before.Conditions, "Ready") {
		environmentTimeToReady.Observe(time.Since(env.CreationTimestamp.Time).Seconds())
	}
```

Note the asymmetry, because it's a real modelling decision rather than an accident: the counter fires on every
failure, the histogram only on the transition.

Check it locally before going further:

```bash
make run
# in another terminal
curl -s localhost:8080/metrics | grep environment_
```

The manager serves metrics on `:8080` by default with `metrics-bind-address`. If you get nothing, the flag is
`0` (disabled) — pass `--metrics-bind-address=:8080`.

## Part B: Prometheus, via GitOps

**File: `gitops/platform/prometheus.yaml`**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kube-prometheus-stack
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-1"   # CRDs before anything that uses them
spec:
  project: default
  source:
    repoURL: https://prometheus-community.github.io/helm-charts
    chart: kube-prometheus-stack
    targetRevision: "77.*"
    helm:
      values: |
        # Keep it small — this is a laptop.
        grafana:
          enabled: true
          adminPassword: admin
        prometheus:
          prometheusSpec:
            retention: 24h
            resources:
              requests: {cpu: 100m, memory: 512Mi}
            # Watch rules in every namespace, not just the release's own.
            ruleSelectorNilUsesHelmValues: false
            serviceMonitorSelectorNilUsesHelmValues: false
        alertmanager:
          alertmanagerSpec:
            resources:
              requests: {cpu: 10m, memory: 64Mi}
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true    # these CRDs are enormous. See runbook 0001.
```

`ServerSideApply` is not optional here — the Prometheus operator CRDs are among the largest in the ecosystem
and will hit the annotation-size limit from [runbook 0001](../runbooks/0001-crd-annotation-size-limit.md).

Those two `*SelectorNilUsesHelmValues: false` lines matter more than they look: without them, Prometheus only
picks up rules and ServiceMonitors labelled for its own Helm release, and your rules will sit there being
silently ignored.

**Scrape the controller.** Kubebuilder scaffolded `config/prometheus/monitor.yaml`, commented out. Enable it,
and uncomment `- ../prometheus` in `config/default/kustomization.yaml`.

One local simplification: the scaffold protects `/metrics` with authn/authz, which means the ServiceMonitor
needs a bearer token and TLS config. For a lab, run the manager with `--metrics-secure=false` and a plain
ServiceMonitor. Note in the manifest that production wants the protected version — the point of the exercise
is the SLO, not certificate plumbing.

## Part C: the SLO, written down

**File: `docs/slo/environment-provisioning.md`**

```markdown
# SLO: environment provisioning

**Users:** application teams requesting environments.
**Why:** if provisioning is unreliable, teams stop trusting the paved road and go back to asking humans.

| SLI | Objective | Window |
|---|---|---|
| Provisioning success rate | 99% | 30 days rolling |
| Time to ready | 95% under 120s | 30 days rolling |

**Error budget:** 1% of 30 days. At ~50 environments/month that's half a failed provision — so a single
failure is a meaningful fraction, and two in a month means stop and fix rather than ship more features.

**Not in this SLO:** cluster CPU, node health, pod restarts. Those are diagnosis. Alerting on them produces
pages nobody can act on, and trains people to ignore alerts.
```

**File: `gitops/platform/platform-slos.yaml`**

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: platform-slos
  namespace: monitoring
spec:
  groups:
    - name: environment-provisioning
      rules:
        - alert: EnvironmentProvisioningFailing
          expr: sum(rate(environment_reconcile_failures_total[10m])) by (reason) > 0
          for: 10m
          labels:
            severity: high
          annotations:
            summary: "Environments failing to provision ({{ $labels.reason }})"
            description: >
              Teams cannot self-serve. Reason label matches the Ready condition,
              so `kubectl describe environment` will say the same thing.
            runbook_url: "https://github.com/sirdesmond/paved-road-platform/blob/main/docs/runbooks/0005-provisioning-failing.md"

        - alert: EnvironmentProvisioningSlow
          expr: histogram_quantile(0.95, sum(rate(environment_time_to_ready_seconds_bucket[30m])) by (le)) > 120
          for: 15m
          labels:
            severity: warning
          annotations:
            summary: "p95 time-to-ready above the 120s objective"
            runbook_url: "https://github.com/sirdesmond/paved-road-platform/blob/main/docs/runbooks/0005-provisioning-failing.md"

        - alert: EnvironmentControllerDown
          expr: up{job=~".*environment-controller.*"} == 0
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "The environment controller is not being scraped"
            description: >
              Nothing is reconciling. Existing environments keep running, but
              nobody can create, change or delete one.
            runbook_url: "https://github.com/sirdesmond/paved-road-platform/blob/main/docs/runbooks/0005-provisioning-failing.md"
```

Three alerts, and notice what's missing: nothing about memory, disk or restarts. Every one of these
corresponds to something a user would notice.

**`runbook_url` on every alert.** An alert without one is a bug — the person receiving it at 3am has to
reconstruct your intent from an expression.

## Part D: the runbook that gets used

Write `docs/runbooks/0005-provisioning-failing.md` in the same shape as the four you already have. The
structure that makes a runbook actually get used:

- **First five minutes** — literal commands to paste, no prose. Someone woken up should not be reading.
- **Common causes as a table**, symptom → likely cause → action.
- **Mitigation before diagnosis.** Restore service, then investigate. A runbook that starts with "understand the root cause" is a runbook for a calm afternoon.
- **Escalation** — who, and when to stop trying.

Start it with:

```bash
kubectl get env -o wide
kubectl -n environment-controller-system logs deploy/environment-controller-controller-manager --tail=50 | grep -i error
kubectl -n argocd get app | grep -v Synced
```

You've already got most of the content: your existing runbooks 0002–0004 cover three real provisioning
failures. Link to them from the causes table rather than duplicating.

---

## Checkpoint

```bash
kubectl -n monitoring get pods
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090
```

In Prometheus at `localhost:9090`:

```promql
environment_time_to_ready_seconds_bucket        # your histogram exists
sum(rate(environment_reconcile_failures_total[10m])) by (reason)
```

Then **make it fire**, because an alert you've never seen fire is an alert you don't have:

```bash
kubectl create namespace squatter
kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata: {name: squatter}
spec:
  owner: {team: search, contact: "#team-search"}
  tier: dev
EOF
```

The adoption guard rejects it, `failed()` increments with reason `NamespaceFailed`, and after ten minutes
`EnvironmentProvisioningFailing` fires with that reason attached. Check Status → Alerts in Prometheus.

Then follow your own runbook from the alert and see whether the first five minutes actually get you to the
cause. That's the only real test of a runbook.

## If it went wrong

| What you see | Usually means |
|---|---|
| Rules exist but Prometheus ignores them | `ruleSelectorNilUsesHelmValues: false` missing, so it only watches its own release's rules. |
| No `environment_` metrics | Manager metrics disabled (`--metrics-bind-address=0`), or the ServiceMonitor isn't matching the service. |
| `up` has no series for the controller | ServiceMonitor not enabled in kustomize, or metrics still protected and the scrape is 401ing. |
| CRDs fail to install | Annotation size limit. `ServerSideApply=true`. |
| Prometheus OOMs on kind | Drop retention to 6h and lower the memory request. |

## Reflection

1. Your error budget is 1% of 30 days and you burned it in one incident. What should that change about what you ship next month, and who decides?
2. `EnvironmentProvisioningFailing` fires with reason `NamespaceFailed`. Is that a platform incident or one team's mistake — and should the same alert cover both?
3. You now alert on time-to-ready. Whose fault is a slow provision when the cause is the cluster having no capacity — and does an SLO you can't fully control still belong to you?
4. Which of your five runbooks would actually help someone who isn't you? Be honest about which ones are notes.

Question 2 is the one that decides whether this alert survives its first month. An alert that fires for
somebody else's typo gets muted, and a muted alert is worse than no alert.
