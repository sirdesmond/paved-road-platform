# Runbook: environment provisioning failing

**Alert:** `EnvironmentReconcileFailing` · **Severity:** high (blocks teams self-serving) · **SLO:** provisioning success rate

## What this means

One or more `Environment` resources are not reaching `Ready`. Teams experience this as "I merged my PR and
nothing happened" — and their fallback is to ask a platform engineer, which is exactly the behaviour the
platform exists to eliminate. Treat sustained provisioning failure as a trust incident, not just a bug.

## First 5 minutes

```bash
# Which environments are stuck, and for how long?
kubectl get environments -A --sort-by=.metadata.creationTimestamp

# What does the controller say about one of them?
kubectl describe environment <name> -n <ns>

# Controller health and recent errors
kubectl -n platform-system logs deploy/environment-controller --tail=100
kubectl -n platform-system get pods -l app=environment-controller
```

## Common causes

| Symptom in logs/status | Likely cause | Action |
|---|---|---|
| `exceeded quota` / admission denied | Cluster or namespace capacity exhausted | Check headroom; scale node group or reject with clear guidance to the team |
| `no matches for kind` | A CRD the controller depends on isn't installed (sync-wave ordering) | Verify the Argo CD app is Synced; check CRDs applied server-side |
| `forbidden` on a resource | Controller RBAC missing a permission (often after adding a new default) | Compare the controller ClusterRole against what the reconciler now creates |
| Gateway/route errors | Envoy Gateway unhealthy or hostname conflict | See the Envoy Gateway runbook; check for duplicate hostnames |
| Reconcile loop with no error | A default referencing something that doesn't exist yet (ordering) | Check `status.conditions` for the stalled step |
| All environments failing at once | Bad controller rollout | **Roll back the controller first**, diagnose after |

## Mitigation

- **If it started right after a controller deploy:** roll back immediately — don't debug in front of blocked teams.
  ```bash
  kubectl -n platform-system rollout undo deploy/environment-controller
  ```
- **If it's capacity:** add capacity if trivial; otherwise communicate to affected teams with an ETA rather than letting PRs sit silently.
- **If it's one environment only:** it is not a platform incident. Fix it, and note whether the failure mode should have been caught at validation time in `platform-api` — validation failures are far cheaper than reconcile failures.

## Communication

Post in the platform channel with: how many environments affected, which teams, whether new provisioning
works, and an ETA. Teams tolerate outages; they don't tolerate silence, and silence is what sends them back
to asking humans directly.

## Follow-up

- Should this have failed at request time instead of reconcile time? If yes, add the check to `platform-api`.
- Did the alert give on-call enough context to start? If not, fix the alert or this runbook.
- Open a postmortem if teams were blocked for more than one hour ([template](./postmortem-template.md)).
