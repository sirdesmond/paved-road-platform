# 03 — Finalizers and clean teardown

**Mode:** faded
**Time:** two hours or so
**You'll end up with:** an `Environment` that cleans up something garbage collection can't see, and a clear
sense of when a finalizer is the wrong tool.

Assumes [02](./02-reconciler.md) passes its checkpoint.

---

## Files you'll touch

Same single file as example 02, plus a namespace that has to exist. Run commands from
`environment-controller/`.

```
environment-controller/
├── internal/controller/environment_controller.go   ← finalizer + registry logic
├── cmd/main.go                                     ← the --registry-namespace flag
└── config/manager/manager.yaml                     ← POD_NAMESPACE via downward API
```

```bash
kubectl create namespace platform-system    # where the shared index lives
```

## First question: do you actually need one?

Almost every finalizer bug I've seen came from adding one that wasn't needed. So start here.

**Ownership already handles everything inside the cluster.** Your namespace, quota and network policy all
carry a controller reference, so deleting the `Environment` deletes them, transitively, without you writing a
line of teardown code. That's example 02, and it's the right answer whenever it's available.

**A finalizer is for things garbage collection cannot see.** Cloud resources, records in another system, a
Git branch, a registration in a service catalogue. Anything where "delete the Kubernetes object" doesn't
delete the real thing.

**And it has a real cost.** A finalizer is a promise that your controller will always be able to finish
cleaning up. If it can't — bad credentials, a dead dependency, a bug, or the controller simply not running —
the object sits in `Terminating` forever and someone has to intervene by hand. You've traded "leaked
resource" for "stuck object", which is the better trade only when the leak actually matters.

The rule I'd apply: **if garbage collection can do it, let it. Add a finalizer only for state that outlives
the cluster's knowledge of the object.**

## So what needs one here?

Right now, nothing in-cluster. Which makes this a slightly artificial exercise unless we pick something real,
so here's the smallest honest example.

[ADR-0003](../adr/0003-no-portal-or-crossplane-in-v1.md) says we skip a catalogue UI, and that ownership is
instead answerable from a generated index. Build that index as a ConfigMap in the platform's own namespace,
listing every environment and its owning team.

That ConfigMap **cannot** be owned by an `Environment`:

- Cross-namespace owner references are disallowed. A namespaced dependent must live in the same namespace as a namespaced owner.
- Your `Environment` is cluster-scoped, so it *could* own a namespaced object — but the index isn't per-environment. It's one shared object that many environments write into. Ownership is one-parent-per-object; that isn't the shape here.

So when an `Environment` goes away, someone has to remove its entry. Garbage collection can't. That's your
finalizer.

The same shape applies to everything you'll add later — a DNS record, a Route 53 zone entry, a row in a cost
system — so getting the pattern right on something local and free is worth an hour.

## The deletion lifecycle

Worth understanding before you write anything, because the API doesn't behave the way people assume.

When you `kubectl delete` an object with finalizers, **it isn't deleted**. The API server sets
`metadata.deletionTimestamp` and leaves the object in place. It stays there, visible, until the finalizer list
is empty — only then does it actually get removed from storage, and only then does garbage collection start
deleting its dependents.

Three consequences:

1. Your reconciler still gets called for objects being deleted. You have to check `deletionTimestamp` and branch, or you'll cheerfully re-create everything you were meant to be tearing down.
2. During cleanup, the children still exist, because the owner hasn't actually been deleted yet. If your cleanup needs to look at them, it can.
3. Removing the finalizer is the *last* thing you do. Remove it before cleanup succeeds and you've lost your only chance.

## Step 1: the finalizer constant

```go
// Finalizer names must be domain-qualified — the API server rejects bare strings.
const environmentFinalizer = "platform.internal/finalizer"
```

## Step 2: branch in Reconcile

Goes immediately after the `Get`, before any of the reconcile calls:

```go
	// Being deleted: clean up, then release.
	if !env.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &env)
	}

	// Not being deleted: make sure we're registered to be told about it.
	if !controllerutil.ContainsFinalizer(&env, environmentFinalizer) {
		controllerutil.AddFinalizer(&env, environmentFinalizer)
		if err := r.Update(ctx, &env); err != nil {
			return ctrl.Result{}, err
		}
	}
```

Note this is `r.Update`, not `r.Status().Update` — finalizers live in `metadata`, not status. And it triggers
another reconcile, which is fine and expected.

## Step 3: the delete path (your turn)

```go
func (r *EnvironmentReconciler) reconcileDelete(ctx context.Context, env *platformv1alpha1.Environment) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(env, environmentFinalizer) {
		// Someone else already released it, or we never added it. Nothing owed.
		return ctrl.Result{}, nil
	}

	// TODO(you): call your deregister function.
	//
	// If it fails, return the error and DO NOT remove the finalizer. The object
	// stays in Terminating and you get retried with backoff. That's the finalizer
	// doing its job, however alarming it looks in kubectl.

	// TODO(you): once cleanup succeeded, remove the finalizer and update.
	// This is the point of no return — after this the object is gone and you
	// will never be called about it again.
}
```

## Step 4: the index itself (your turn, less help)

Write `reconcileRegistry` (called from the normal path) and `deregister` (called from the delete path). One
ConfigMap, `environment-index`, in the platform namespace, with one entry per environment: key is the
environment name, value is the owning team.

Three things to get right, and they're the whole exercise:

**Cleanup must be idempotent.** `deregister` will be called more than once — a retry after a partial failure,
a controller restart mid-delete. Removing an entry that isn't there is a success, not an error. Likewise, if
the ConfigMap itself is gone, that's success: the goal is "no entry for this environment", and it's met.

**Handle the ConfigMap not existing.** On a fresh cluster the first environment has to create it.

**Concurrent writes will conflict.** Several environments reconciling at once all want to patch the same
object, and you'll get `Conflict` errors from optimistic concurrency. The lazy fix is to return the error and
let the backoff sort it out, which does work. The better fix is `retry.RetryOnConflict` from
`k8s.io/client-go/util/retry`, which re-fetches and re-applies. Worth doing properly — it's the same pattern
you'll need any time two controllers touch one object.

---

## Checkpoint

```bash
make build && make run
```

Normal path, and confirm the finalizer gets attached:

```bash
kubectl apply -f config/samples/platform_v1alpha1_environment.yaml
kubectl get env checkout-staging -o jsonpath='{.metadata.finalizers}{"\n"}'
kubectl get cm environment-index -n <platform-ns> -o yaml
```

Deletion, which is the interesting bit:

```bash
kubectl delete env checkout-staging
kubectl get cm environment-index -n <platform-ns> -o yaml   # entry gone
kubectl get env checkout-staging                            # gone
kubectl get ns checkout-staging                             # terminating, then gone
```

Now prove the finalizer actually blocks. Stop the controller (`Ctrl-C` on `make run`), then:

```bash
kubectl delete env search-dev     # returns, but doesn't finish
kubectl get env search-dev        # still there, with a deletionTimestamp
kubectl get env search-dev -o jsonpath='{.metadata.deletionTimestamp}{"\n"}'
```

Restart the controller and watch it complete. **This is the single most important thing to see with your own
eyes**, because it's what "stuck in Terminating" actually looks like from the inside, and it's why a finalizer
is a commitment rather than a feature.

## Getting out of a stuck delete

You'll need this, so learn it now rather than during an incident:

```bash
kubectl patch env stuck-one --type=merge -p '{"metadata":{"finalizers":[]}}'
```

The object disappears immediately. It also means whatever the cleanup would have done never happened — the
index entry leaks, and in a real system so would a DNS record or a cloud resource. It's a legitimate
last resort and a terrible habit.

## If it went wrong

| What you see | Usually means |
|---|---|
| Object deletes instantly, no cleanup | Finalizer never added. Check the add branch runs before anything can delete. |
| Stuck in Terminating forever | Cleanup keeps failing, or the controller isn't running. Check its logs before you patch anything. |
| Everything gets re-created during delete | Missing the `deletionTimestamp` check, so the normal path is still running. |
| `Conflict` errors under load | Concurrent writes to the index. `retry.RetryOnConflict`. |
| `metadata.finalizers: Invalid value` | Not domain-qualified. Needs a slash. |

---

## Reference implementation

Try it yourself first — the three difficulties (idempotent cleanup, missing ConfigMap, write conflicts) are
the whole point. If you're stuck, or want to compare:

```go
const registryName = "environment-index"

// On the reconciler struct, set from a flag in main.go rather than hardcoded:
//   RegistryNamespace string

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

// reconcileRegistry records this environment in the shared index.
//
// Deliberately no owner reference: the index is one object shared by every
// environment, and ownership is one-parent-per-object. It would also mean the
// first environment deleted takes the whole index with it.
func (r *EnvironmentReconciler) reconcileRegistry(ctx context.Context, env *platformv1alpha1.Environment) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: registryName, Namespace: r.RegistryNamespace},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
			if cm.Data == nil {
				cm.Data = map[string]string{}
			}
			cm.Data[env.Name] = fmt.Sprintf("team=%s tier=%s contact=%s",
				env.Spec.Owner.Team, env.Spec.Tier, env.Spec.Owner.Contact)
			return nil
		})
		return err
	})
}

// deregister removes this environment's entry. Must be safe to call repeatedly:
// it runs on every retry of a failed delete, and after a restart mid-deletion.
func (r *EnvironmentReconciler) deregister(ctx context.Context, env *platformv1alpha1.Environment) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cm corev1.ConfigMap
		err := r.Get(ctx, client.ObjectKey{Name: registryName, Namespace: r.RegistryNamespace}, &cm)
		if apierrors.IsNotFound(err) {
			return nil // no index, so no entry. Goal already met.
		}
		if err != nil {
			return err
		}
		if _, present := cm.Data[env.Name]; !present {
			return nil // already removed. Also success.
		}
		delete(cm.Data, env.Name)
		return r.Update(ctx, &cm)
	})
}

func (r *EnvironmentReconciler) reconcileDelete(ctx context.Context, env *platformv1alpha1.Environment) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(env, environmentFinalizer) {
		return ctrl.Result{}, nil
	}

	// Cleanup first. If this fails we keep the finalizer and get retried:
	// the object stays in Terminating, which is the finalizer working.
	if err := r.deregister(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	// Point of no return — after this we're never called about it again.
	controllerutil.RemoveFinalizer(env, environmentFinalizer)
	return ctrl.Result{}, r.Update(ctx, env)
}
```

Extra imports: `apierrors "k8s.io/apimachinery/pkg/api/errors"` and `"k8s.io/client-go/util/retry"`.

The two ideas worth taking from this: `deregister` treats both "no ConfigMap" and "no entry" as success,
because the goal is *absence* rather than *having performed a deletion* — that's what makes it safe on
retries. And `RetryOnConflict` re-fetches and re-applies instead of failing, which you need whenever more
than one reconcile can write the same object.

A finalizer with no cleanup behind it is worse than no finalizer: all of the stuck-object risk, none of the
benefit. If you decide the index isn't worth it, remove the finalizer too.

## Before you move on

Write the runbook. `docs/runbooks/environment-stuck-terminating.md`: what the alert looks like, how to tell a
failing cleanup from a controller that isn't running, the diagnostic commands, and the force-removal escape
hatch with its consequences spelled out.

You've just built the failure mode, so you're the best-placed person to document it, and this one *will*
happen in production.

## Reflection

1. You add a finalizer today. A year later that controller is deleted from the cluster while environments still exist. What happens to them, and what should you have done first?
2. Your cleanup calls an external API that's been down for an hour. Is "stuck in Terminating" the right behaviour, or should it give up? What would giving up cost?
3. Ownership deletes things when the parent goes. Finalizers run before. Why can't you use ownership to guarantee ordering — say, deregistering from a load balancer before the pods vanish?

Next: [04 — tier defaults and a validation webhook](./04-tier-defaults.md).
