# 02 — The reconciler

**Mode:** faded (structure and the hard parts given, gaps marked `TODO(you)`)
**Time:** two to three hours, and it should feel slower than 01
**You'll end up with:** an `Environment` that creates a namespace, a resource quota and a default-deny
network policy, sets ownership so cleanup works, and reports honest status.

Assumes [01](./01-scaffold-and-api.md) is passing its checkpoint.

---

## What you're building

```
Environment "checkout-staging"
   creates  →  Namespace       checkout-staging
               ResourceQuota   checkout-staging/quota   (from spec.resources)
               NetworkPolicy   checkout-staging/default-deny-ingress
   sets     →  status.namespace, status.conditions[Ready], status.observedGeneration
```

Same shape as the Crossplane demo in the sibling repo, except this time you're writing the controller instead
of configuring one. That difference is the entire point of this example.

## The mental model first

A reconciler is not a "create things" function. It answers one question, over and over: **given the world as
it is right now, what single step moves it closer to the spec?** Then it returns, and gets called again.

Three consequences that trip people up:

- **It runs many times, including when nothing changed.** Every operation has to be safe to repeat. `Create` is not; create-or-update is.
- **It can be interrupted anywhere.** Halfway through making three objects, the process can die. On the next call you have to cope with "namespace exists, quota doesn't".
- **It's not a transaction.** There's no rollback. Partial state is normal, and your job is to converge from wherever you are.

If you write it as a script that runs top to bottom on creation, it'll work in the demo and fall apart the
first time something is slow or already exists.

## Step 1: RBAC markers

Above the `Reconcile` method in `internal/controller/environment_controller.go`:

```go
// +kubebuilder:rbac:groups=platform.internal,resources=environments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.internal,resources=environments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.internal,resources=environments/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
```

These generate the ClusterRole. Forget one and you'll get a `forbidden` error at runtime that reads like a
bug in your logic but isn't — worth recognising the shape of that error now so you don't lose an hour to it
later.

## Step 2: the reconcile skeleton

```go
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var env platformv1alpha1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		// Not found means it was deleted. Nothing to do: ownership handles the cleanup.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nsName := env.Name

	// 1. Namespace
	if err := r.reconcileNamespace(ctx, &env, nsName); err != nil {
		return r.failed(ctx, &env, "NamespaceFailed", err)
	}

	// 2. Quota
	if err := r.reconcileQuota(ctx, &env, nsName); err != nil {
		return r.failed(ctx, &env, "QuotaFailed", err)
	}

	// 3. Network policy
	// TODO(you): call reconcileNetworkPolicy, same error handling as above.

	log.Info("environment reconciled", "namespace", nsName)
	return ctrl.Result{}, r.ready(ctx, &env, nsName)
}
```

Note what the not-found branch does. Deletion is handled by the ownership you set in step 3, not by code
here. Example 03 covers when that isn't enough (cloud resources, anything outside the cluster) and you need a
finalizer.

## Step 3: create-or-update, and ownership

This is the piece worth understanding properly. `controllerutil.CreateOrUpdate` fetches the object, runs your
mutate function against it, and either creates or patches. Your mutate function must be safe to run against
both an empty object and an existing one.

```go
func (r *EnvironmentReconciler) reconcileNamespace(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels["platform.internal/team"] = env.Spec.Owner.Team
		ns.Labels["platform.internal/tier"] = env.Spec.Tier
		ns.Labels["app.kubernetes.io/managed-by"] = "environment-controller"

		// Ownership: when the Environment goes, so does this.
		return controllerutil.SetControllerReference(env, ns, r.Scheme)
	})
	return err
}
```

Two things to notice.

**The mutate function only sets fields it owns.** It doesn't build a whole Namespace object and assign it,
because that would wipe anything else that's been added to the live object (annotations from other
controllers, for instance). Set your fields, leave the rest.

**`SetControllerReference` is what makes deletion work.** It writes an owner reference, and Kubernetes'
garbage collector deletes children when the parent goes. This is also why the CRD had to be cluster-scoped in
01: a namespaced owner can't own a cluster-scoped Namespace, and the GC would quietly ignore it.

## Step 4: the quota (your turn)

```go
func (r *EnvironmentReconciler) reconcileQuota(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: nsName},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		// TODO(you): set quota.Spec.Hard from env.Spec.Resources.
		//
		// You want limits on: requests.cpu, requests.memory, limits.cpu,
		// limits.memory and pods.
		//
		// Hard is a corev1.ResourceList, which is map[ResourceName]resource.Quantity.
		// Your spec fields are already Quantity, so no parsing needed. Pods is an
		// int32, so look at resource.MustParse or *resource.NewQuantity.
		//
		// Then set the controller reference, same as the namespace.
		return nil
	})
	return err
}
```

Question worth sitting with while you write it: should `requests.cpu` and `limits.cpu` be the same value?
There's a real argument either way, and whichever you choose is a platform policy decision you're making on
behalf of every team. Write down why in a comment.

## Step 5: the network policy (your turn, less help)

Create a `NetworkPolicy` called `default-deny-ingress` in the namespace, selecting all pods, with
`policyTypes: [Ingress]` and no rules. Same create-or-update shape, same owner reference.

The empty `PodSelector{}` means "every pod", which is the part people get wrong first time.

## Step 6: honest status

Status is how everything else finds out whether this worked, so it's worth more care than it usually gets.

```go
func (r *EnvironmentReconciler) ready(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	env.Status.Namespace = nsName
	env.Status.ObservedGeneration = env.Generation
	meta.SetStatusCondition(&env.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "namespace, quota and network policy are in place",
		ObservedGeneration: env.Generation,
	})
	return r.Status().Update(ctx, env)
}

func (r *EnvironmentReconciler) failed(ctx context.Context, env *platformv1alpha1.Environment, reason string, cause error) (ctrl.Result, error) {
	// TODO(you): set Ready=False with this reason and cause.Error() as the message,
	// update status, then return the error so the manager requeues with backoff.
	//
	// Careful: if the status update itself fails you still want to return the
	// original error, not the update error. Losing the real cause here is a
	// genuinely annoying bug to track down later.
	return ctrl.Result{}, cause
}
```

`r.Status().Update` rather than `r.Update`, because status is a subresource (that's what
`+kubebuilder:subresource:status` bought you in 01). Using the wrong one is a common early mistake and the
symptom is confusing: your spec changes get reverted.

### Derived status: the TTL from example 01

If you did the TTL exercise, this is where it pays off, and it demonstrates two rules worth keeping.

**Compute defaults, don't write them into the spec.** The TTL default depends on tier, so it can't be a
schema default. The temptation is to have the reconciler set `env.Spec.TTL` when it's empty. Don't: the spec
belongs to the user, and once you've written to it you can't distinguish "they asked for a week" from "we
filled it in". Compute the effective value each pass and persist only what you derive from it.

**Derive from stable inputs.** Expiry is `creationTimestamp + ttl`, never `time.Now() + ttl`. Anything based
on "now" produces a different answer every reconcile, so status gets rewritten, which triggers a watch, which
reconciles again. That's the most common cause of a controller that spins at 100% doing nothing.

```go
const defaultTTL = 168 * time.Hour

func effectiveTTL(env *platformv1alpha1.Environment) *metav1.Duration {
	if env.Spec.Tier == "prod" {
		return nil // production doesn't expire
	}
	if env.Spec.TTL != nil {
		return env.Spec.TTL
	}
	return &metav1.Duration{Duration: defaultTTL}
}
```

Then in `ready()`:

```go
	if ttl := effectiveTTL(env); ttl != nil {
		expiry := metav1.NewTime(env.CreationTimestamp.Add(ttl.Duration))
		env.Status.ExpiresAt = &expiry
	} else {
		env.Status.ExpiresAt = nil
	}
```

The `else` is the part people leave out. Without it, an environment promoted from staging to prod keeps a
stale expiry, and whatever reaps environments later happily deletes production.

Worth deciding explicitly: a `ttl` set on a prod environment is currently ignored without comment. Silently
discarding user input is unkind. Reject it at admission with a CEL rule, or surface a condition saying it
isn't honoured — either is fine, but pick one.

## Step 7: watch what you own

In `SetupWithManager`:

```go
func (r *EnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Environment{}).
		Owns(&corev1.Namespace{}).
		// TODO(you): own ResourceQuota and NetworkPolicy too.
		Named("environment").
		Complete(r)
}
```

`Owns` means: when one of these children changes, reconcile its parent. That's what makes the controller
self-healing. Delete the quota by hand and it comes back, because the change triggers a reconcile of the
`Environment` that owns it.

---

## Checkpoint

```bash
make manifests generate
make install
make run   # runs the controller locally against your kind cluster
```

In another terminal:

```bash
kubectl apply -f config/samples/platform_v1alpha1_environment.yaml
kubectl get env
```

`Ready` should be `True` and `Namespace` should be populated. Then:

```bash
kubectl get ns checkout-staging --show-labels
kubectl get resourcequota,networkpolicy -n checkout-staging
kubectl describe resourcequota quota -n checkout-staging
```

The quota should show the values from your spec.

**Now test the properties that actually matter.**

Self-healing:
```bash
kubectl delete resourcequota quota -n checkout-staging
sleep 3
kubectl get resourcequota -n checkout-staging   # it's back
```

Idempotency:
```bash
kubectl annotate env checkout-staging poke=1 --overwrite
# watch the logs: it reconciles again and changes nothing
```

Update propagation:
```bash
kubectl patch env checkout-staging --type=merge -p '{"spec":{"resources":{"pods":50}}}'
kubectl get resourcequota quota -n checkout-staging -o jsonpath='{.spec.hard.pods}{"\n"}'   # 50
```

Cleanup via ownership:
```bash
kubectl delete env checkout-staging
kubectl get ns checkout-staging   # terminating, then gone
```

If all five pass, commit. You've built the thing the sibling repo used Crossplane for.

## If it went wrong

| What you see | Usually means |
|---|---|
| `forbidden` on namespaces or quotas | Missing RBAC marker, or you didn't re-run `make manifests`. Note `make run` uses *your* kubeconfig, so RBAC problems often only appear once deployed in-cluster. |
| Namespace created, quota isn't | An error is being swallowed. Check you're returning it rather than logging it. |
| Deleting the Environment leaves the namespace | Owner reference missing or wrong. `kubectl get ns X -o jsonpath='{.metadata.ownerReferences}'`. |
| Status never updates | Using `r.Update` instead of `r.Status().Update`, or the subresource marker is missing. |
| Reconcile loops constantly | You're writing to the object on every pass, which triggers a watch, which reconciles. Only update status when something actually changed. |
| Quota rejects your Quantity | Pods is a count, not a compute resource. `*resource.NewQuantity(int64(n), resource.DecimalSI)`. |

---

## Reflection

Worth answering properly before example 03, because these are interview questions:

1. Your controller made three objects and died after the second one. What happens next, and why is that fine?
2. Someone edits the quota by hand to give themselves more CPU. How long does that survive, and what mechanism undoes it?
3. Why doesn't a namespace stuck in `Terminating` mean your controller is broken?
4. You now have three ways to enforce a quota: this controller, an admission policy, or the request-time check in `platform-api`. What does each catch that the others don't? ([ADR-0004](../adr/0004-policy-enforcement-layers.md) has an opinion.)

Next up: finalizers and the deletion cases that ownership can't handle.
