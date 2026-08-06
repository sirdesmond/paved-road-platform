package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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
