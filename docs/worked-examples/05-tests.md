# 05 — Tests with envtest

**Mode:** worked
**Time:** two hours
**You'll end up with:** controller tests that run in CI, covering tier defaults, the CEL rules, and a
reconcile that fails halfway.

Assumes [04](./04-tier-defaults.md) works.

---

## Files you'll touch

Run commands from `environment-controller/`.

```
environment-controller/
├── internal/controller/environment_controller_test.go   ← replace with the tests below
├── internal/controller/suite_test.go                    (already scaffolded; check it)
└── Makefile                                             (optional: ARGS passthrough, cover targets)
```

Plus `.github/workflows/test.yml` at the **repo root** if you want CI.

## What envtest actually is

`envtest` starts a **real `kube-apiserver` and `etcd`** as local processes and points your client at them.
That's it. No kubelet, no scheduler, no `kube-controller-manager`.

What you get is genuinely valuable: real API semantics. Your CRD schema, your validation markers, your CEL
rules, defaulting, status subresources, optimistic concurrency — all the things a fake client mocks away and
gets subtly wrong.

What you don't get matters just as much:

- **No garbage collection.** There's no controller-manager, so owner references are *recorded* but dependents are never deleted. You cannot test your teardown here.
- **Namespaces never finish deleting.** Same reason — nothing runs the namespace lifecycle, so they sit in `Terminating` forever.
- **Nothing schedules.** Pods stay `Pending`, so a ResourceQuota never actually gets consumed.

That first one catches people out badly, because deletion is exactly what you'd want to test after example 03.
The rule to hold onto: **envtest tests what your controller does, not what Kubernetes does in response.** You
assert that the owner reference is set correctly; you trust Kubernetes to act on it, and you verify that once,
by hand, on kind.

## Setup

Kubebuilder already scaffolded `internal/controller/suite_test.go`, which starts envtest and loads your CRDs
from `config/crd/bases`. Two things to check:

```bash
grep -n "CRDDirectoryPaths" internal/controller/suite_test.go
make manifests   # tests load the generated CRD, so stale manifests mean stale schemas
```

The binaries download on first run via `setup-envtest`, which is why `make test` pauses the first time.

## Style: call Reconcile directly

The scaffold offers two approaches. You can start the whole manager and poll with `Eventually`, or you can
construct the reconciler and call `Reconcile` yourself.

Call it directly. It's deterministic, it's fast, there's no polling and no flakes, and it matches the mental
model: reconcile is a function from current state to one step of convergence. When a test fails you know
exactly which pass failed.

## The tests

Replace `internal/controller/environment_controller_test.go` with this.

```go
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
)

var _ = Describe("Environment controller", func() {
	ctx := context.Background()

	newReconciler := func() *EnvironmentReconciler {
		return &EnvironmentReconciler{
			Client:            k8sClient,
			Scheme:            k8sClient.Scheme(),
			RegistryNamespace: "default", // exists in envtest
		}
	}

	// reconcile runs enough passes to settle. The first adds the finalizer and
	// returns early, so a single pass never does the actual work — which the
	// tests will remind you of loudly if you forget.
	reconcileN := func(r *EnvironmentReconciler, name string, passes int) error {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}
		for i := 0; i < passes; i++ {
			if _, err := r.Reconcile(ctx, req); err != nil {
				return err
			}
		}
		return nil
	}

	newEnv := func(name, tier string) *platformv1alpha1.Environment {
		return &platformv1alpha1.Environment{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: platformv1alpha1.EnvironmentSpec{
				Owner: platformv1alpha1.Owner{Team: "search", Contact: "#team-search"},
				Tier:  tier,
			},
		}
	}

	It("provisions a namespace, quota and network policy", func() {
		env := newEnv("test-provision", "dev")
		Expect(k8sClient.Create(ctx, env)).To(Succeed())

		r := newReconciler()
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: env.Name}}

		// First pass: adds the finalizer, returns early.
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var afterFirst platformv1alpha1.Environment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name}, &afterFirst)).To(Succeed())
		Expect(afterFirst.Finalizers).To(ContainElement("platform.internal/finalizer"))
		Expect(afterFirst.Status.Namespace).To(BeEmpty(), "no work should have happened yet")

		// Second pass: the actual work.
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var ns corev1.Namespace
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name}, &ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue("platform.internal/team", "search"))
		Expect(ns.OwnerReferences).To(HaveLen(1))
		Expect(ns.OwnerReferences[0].Controller).To(HaveValue(BeTrue()))

		var quota corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "quota", Namespace: env.Name}, &quota)).To(Succeed())

		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default-deny-ingress", Namespace: env.Name}, &np)).To(Succeed())
		Expect(np.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeIngress))

		var done platformv1alpha1.Environment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name}, &done)).To(Succeed())
		Expect(done.Status.Namespace).To(Equal(env.Name))
		Expect(done.Status.ObservedGeneration).To(Equal(done.Generation))
	})

	It("applies the tier default when the team asks for nothing", func() {
		By("dev getting the small ceiling")
		dev := newEnv("test-tier-dev", "dev")
		Expect(k8sClient.Create(ctx, dev)).To(Succeed())
		Expect(reconcileN(newReconciler(), dev.Name, 2)).To(Succeed())

		var devQuota corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "quota", Namespace: dev.Name}, &devQuota)).To(Succeed())
		Expect(devQuota.Spec.Hard.Name(corev1.ResourceRequestsCPU, resource.DecimalSI).String()).To(Equal("2"))

		By("prod getting the large one")
		prod := newEnv("test-tier-prod", "prod")
		Expect(k8sClient.Create(ctx, prod)).To(Succeed())
		Expect(reconcileN(newReconciler(), prod.Name, 2)).To(Succeed())

		var prodQuota corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "quota", Namespace: prod.Name}, &prodQuota)).To(Succeed())
		Expect(prodQuota.Spec.Hard.Name(corev1.ResourceRequestsCPU, resource.DecimalSI).String()).To(Equal("32"))
	})

	It("merges an override field by field", func() {
		env := newEnv("test-override", "dev")
		env.Spec.Resources = platformv1alpha1.Resources{CPU: resource.MustParse("1")}
		Expect(k8sClient.Create(ctx, env)).To(Succeed())
		Expect(reconcileN(newReconciler(), env.Name, 2)).To(Succeed())

		var quota corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "quota", Namespace: env.Name}, &quota)).To(Succeed())

		// CPU is the override; memory and pods still come from the dev tier.
		Expect(quota.Spec.Hard.Name(corev1.ResourceRequestsCPU, resource.DecimalSI).String()).To(Equal("1"))
		Expect(quota.Spec.Hard.Name(corev1.ResourceRequestsMemory, resource.BinarySI).String()).To(Equal("4Gi"))
		Expect(quota.Spec.Hard.Name(corev1.ResourcePods, resource.DecimalSI).String()).To(Equal("10"))
	})

	It("rejects a ttl on a production environment", func() {
		env := newEnv("test-prod-ttl", "prod")
		env.Spec.TTL = &metav1.Duration{Duration: 24 * 60 * 60}
		err := k8sClient.Create(ctx, env)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("production environments do not expire"))
	})

	It("rejects a change of tier", func() {
		env := newEnv("test-immutable", "dev")
		Expect(k8sClient.Create(ctx, env)).To(Succeed())

		env.Spec.Tier = "prod"
		err := k8sClient.Update(ctx, env)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("tier is immutable"))
	})

	It("refuses to adopt a namespace it did not create", func() {
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-squatter"},
		})).To(Succeed())

		env := newEnv("test-squatter", "dev")
		Expect(k8sClient.Create(ctx, env)).To(Succeed())

		r := newReconciler()
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: env.Name}}
		_, _ = r.Reconcile(ctx, req) // finalizer pass
		_, err := r.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())

		var after platformv1alpha1.Environment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name}, &after)).To(Succeed())

		cond := meta.FindStatusCondition(after.Status.Conditions, "Ready")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("NamespaceFailed"))
	})
})
```

Add `"k8s.io/apimachinery/pkg/api/meta"` to the imports for that last test.

## Run them

```bash
make test
```

---

## What these tests are actually buying you

Worth being clear, because "we have tests" is not the same as "we're covered".

**The CEL tests are the highest-value ones**, and they'd be impossible with a fake client. They run against a
real API server, so they verify the rules you wrote in markers actually compile and behave. A typo in a CEL
expression is otherwise a runtime surprise on a cluster.

**The failure test is the one most people skip.** It proves that a broken reconcile reports itself properly —
`Ready=False` with a reason a human can act on. Untested error paths are how you end up with a controller that
fails silently, which is strictly worse than one that fails loudly.

**The two-pass structure documents your control flow.** If someone removes the early return after adding the
finalizer, the first assertion (`Status.Namespace` is empty after one pass) fails and tells them what changed.

**What isn't covered, and you should know it:** deletion. No garbage collection in envtest, so the cascade
you built in examples 02 and 03 is verified by hand on kind, not here. Write that down — an untested path
someone *believes* is tested is more dangerous than one everyone knows isn't.

## CI

```yaml
name: test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: environment-controller/go.mod
      - run: make test
        working-directory: environment-controller
```

`make test` handles `setup-envtest` itself, so there's nothing else to install. Which is the point: the tests
run anywhere Go runs, with no cluster.

## If it went wrong

| What you see | Usually means |
|---|---|
| `no matches for kind "Environment"` | Stale CRDs. `make manifests`, then re-run. |
| CEL tests pass when they shouldn't | Your envtest Kubernetes version predates the CEL feature. Check `ENVTEST_K8S_VERSION` in the Makefile. |
| `namespaces "x" already exists` across tests | Tests share one API server and envtest never deletes namespaces. Use distinct names per test — which is why every name above is prefixed. |
| Quota assertions off by units | `Quantity.String()` normalises: `"1024Mi"` is not the string `"1Gi"`. Compare with `.Cmp()` if you need semantic equality. |
| Nothing happens on the first Reconcile | That's correct. See the two-pass note. |

## Reflection

1. Your test suite is green, and deletion is broken in production. Which of the guarantees you rely on is untested, and how would you have caught it without a cluster?
2. These tests call `Reconcile` directly, so they never exercise the watches in `SetupWithManager`. What class of bug does that miss?
3. The failure test asserts on a condition `Reason` string. Is that too tightly coupled, or is the reason part of your API? What would break for a consumer if you renamed it?

Next: Part 2 — Argo CD, delivery, and the CD flow.
