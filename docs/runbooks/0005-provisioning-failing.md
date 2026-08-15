# Runbook 0005 — Environment provisioning is failing

**Alerts:** `EnvironmentProvisioningFailing`, `EnvironmentProvisioningSlow`, `EnvironmentControllerDown`
**Severity:** high (teams can't self-serve) · critical if the controller is down
**SLO at risk:** [environment provisioning](../slo/environment-provisioning.md) — 99% success, p95 under 120s

## What this means for users

Teams can't create or change environments. Existing environments and their workloads keep running — the
controller only manages the objects, not the traffic — so **this is not a production outage.** It's a
productivity outage, and the failure mode to avoid is treating it as the first one at 3am.

If nobody is blocked right now, this can wait for the morning. Check before escalating.

---

## First five minutes

Paste these. Don't read ahead yet.

```bash
# Which environments are unhappy, and since when
kubectl get env -o wide

# What the controller says
kubectl -n environment-controller-system logs deploy/environment-controller-controller-manager \
  --tail=50 | grep -iE 'error|forbidden|denied'

# Is it even running
kubectl -n environment-controller-system get pods

# Is anything else broken upstream
kubectl -n argocd get app | grep -v Synced
```

Then get the reason, because the alert's `reason` label is the same string the controller writes into the
Ready condition:

```bash
kubectl describe env <name> | tail -20
```

## Common causes

| Reason / symptom | Cause | Action |
|---|---|---|
| `NamespaceFailed` — "already exists and is not managed by this controller" | Someone named an environment after an existing namespace. The adoption guard is working. | Not a platform fault. Tell the requester to pick another name. One occurrence is noise, a pattern means the name collision should be caught at request time in `platform-api`. |
| `NamespaceFailed`, `QuotaFailed` or `RegistryFailed` with `forbidden` | Controller RBAC is missing a permission, usually after adding something new to the reconciler. | `make manifests`, commit `config/rbac/role.yaml`, push. Worked with `make run` because that used *your* kubeconfig. |
| Any reason, with `denied request` from a `ValidatingAdmissionPolicy` | An admission policy is rejecting the controller's own write. If the object also has a finalizer it is now **undeletable**. | [Runbook 0004](./0004-admission-policy-deadlocks-finalizer.md). |
| Objects stuck `Terminating` | Cleanup can't complete, so the finalizer stays. | [Runbook 0004](./0004-admission-policy-deadlocks-finalizer.md), and check `deregister` can reach the registry ConfigMap. |
| Controller pod `ErrImagePull` | Image not present in this cluster. Mutable tags mean "whatever was loaded here at some point". | `kind load docker-image environment-controller:dev --name <cluster>`. |
| Controller `CrashLoopBackOff` on start | Usually the missing `--registry-namespace` / `POD_NAMESPACE`. The guard is deliberate. | Check the startup log; it names the flag. |
| Everything slow, nothing failing | Reconcile queue backed up, or the API server is unhappy. | Check `workqueue_depth` and `controller_runtime_reconcile_time_seconds` in Prometheus. |
| No metrics at all, alert fired on `up == 0` | Scrape broken, not provisioning. | See "the alert lied" below. |

## Mitigation first

Restore service, then investigate. In rough order:

**If it started right after a controller deploy**, roll back and diagnose afterwards:

```bash
kubectl -n environment-controller-system rollout undo deploy/environment-controller-controller-manager
```

**If an admission policy is rejecting the controller's writes**, flip the binding to `Audit` to unblock, then
fix the rule properly:

```bash
kubectl edit validatingadmissionpolicybinding <name>   # validationActions: ["Audit"]
```

That disables a guardrail cluster-wide, so open a ticket to re-enable it in the same breath.

**If one environment is wedged and the rest are fine**, it isn't an incident. Fix that object and move on.

## When the alert lied

`EnvironmentControllerDown` fires on `up == 0`, which means *Prometheus can't scrape it* — not necessarily
that it's down. Two false-alarm causes, both seen:

- The ServiceMonitor isn't selected, because `serviceMonitorSelectorNilUsesHelmValues: false` is missing from the Prometheus values. Scrapes never happened, so `up` has no series.
- Prometheus's ServiceAccount can't read the protected `/metrics` endpoint (403), because the `metrics-reader` ClusterRole isn't bound to it.

Confirm before treating it as an outage:

```bash
kubectl -n environment-controller-system get pods          # is it actually running?
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090
# Status → Targets → look for environment-controller and read the error
```

If the pods are healthy, this is a monitoring failure, not a platform failure. Fix the scrape and stop paging.

## Escalation

There's no one to escalate to on this platform — that's the honest answer for a team of one, and worth
writing down rather than pretending. Stop-and-ask points:

- More than 30 minutes without a cause, and teams are blocked → post in the platform channel with what you've ruled out, and put a manual workaround in place (create the namespace by hand, note it for reconciliation later).
- Data loss is possible (namespaces being deleted unexpectedly) → stop, don't experiment, capture state first: `kubectl get env,ns -o yaml > /tmp/state.yaml`.

## Follow-up

- **Should this have failed earlier?** Most causes above could have been caught at request time in `platform-api` with a better message. If so, that's the fix, not a runbook entry.
- **Did the alert give enough to start?** If you had to go hunting for the reason, add it to the alert annotation.
- **Was it one team's mistake?** `NamespaceFailed` from a name collision isn't a platform incident. If this alert keeps firing for user error, it will get muted — and a muted alert is worse than no alert. Split the rule.
- Postmortem if teams were blocked over an hour ([template](./postmortem-template.md)).

## Notes on this runbook

Written after the failures in runbooks 0002–0004, which is why the causes table is specific: every row is
something that actually happened here rather than something that might. The rows will age — delete ones that
stop being true rather than letting the table become folklore.
