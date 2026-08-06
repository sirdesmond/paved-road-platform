/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	platformv1alpha1 "github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// EnvironmentReconciler reconciles a Environment object
type EnvironmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// RegistryNamespace is where the shared environment index lives.
	// Set from a flag in main.go so it isn't hardcoded to one cluster layout.
	RegistryNamespace string
}

const registryName = "environment-index"
const environmentFinalizer = "platform.internal/finalizer"

// tierDefaults is the platform's opinion about what each tier is worth.
// One table, one place to change it.
var tierDefaults = map[string]platformv1alpha1.Resources{
	"dev":     {CPU: resource.MustParse("2"), Memory: resource.MustParse("4Gi"), Pods: 10},
	"staging": {CPU: resource.MustParse("8"), Memory: resource.MustParse("16Gi"), Pods: 30},
	"prod":    {CPU: resource.MustParse("32"), Memory: resource.MustParse("64Gi"), Pods: 100},
}

// +kubebuilder:rbac:groups=platform.internal,resources=environments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.internal,resources=environments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.internal,resources=environments/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var env platformv1alpha1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		// Not found means it was deleted. Nothing to do: ownership handles the cleanup.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

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
		// Our own update triggers a watch event, so we'll be called straight
		// back with a fresh copy. Doing the real work then keeps one write
		// per reconcile, which is what avoids conflicting with ourselves.
		return ctrl.Result{}, nil
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
	if err := r.reconcileNetworkPolicy(ctx, &env, nsName); err != nil {
		return r.failed(ctx, &env, "NetworkPolicyFailed", err)
	}

	if err := r.reconcileRegistry(ctx, &env); err != nil {
		return r.failed(ctx, &env, "RegistryFailed", err)
	}
	log.Info("environment reconciled", "namespace", nsName)
	return ctrl.Result{}, r.ready(ctx, &env, nsName)
}

func (r *EnvironmentReconciler) reconcileNamespace(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if !ns.CreationTimestamp.IsZero() &&
			ns.Labels["app.kubernetes.io/managed-by"] != "environment-controller" {
			return fmt.Errorf("namespace %q already exists and is not managed by this controller", nsName)
		}

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

func (r *EnvironmentReconciler) reconcileQuota(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: nsName},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		eff := effectiveResources(env)
		cpu := eff.CPU
		mem := eff.Memory

		// Requests and limits share one ceiling: no overcommit in a shared
		// cluster, and one number for a team to reason about. Revisit if
		// teams need burst headroom above their guaranteed floor.
		quota.Spec.Hard = corev1.ResourceList{
			corev1.ResourceRequestsCPU:    cpu,
			corev1.ResourceRequestsMemory: mem,
			corev1.ResourceLimitsCPU:      cpu,
			corev1.ResourceLimitsMemory:   mem,
			corev1.ResourcePods:           *resource.NewQuantity(int64(eff.Pods), resource.DecimalSI),
		}
		return controllerutil.SetControllerReference(env, quota, r.Scheme)
	})
	return err
}

func (r *EnvironmentReconciler) reconcileNetworkPolicy(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-deny-ingress", Namespace: nsName},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		// Empty PodSelector means every pod in the namespace.
		// Ingress in PolicyTypes with no rules means deny all inbound.
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		}
		return controllerutil.SetControllerReference(env, np, r.Scheme)
	})
	return err
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

// effectiveResources merges what the team asked for over the tier default,
// field by field. Asking for CPU shouldn't silently reset your memory.
func effectiveResources(env *platformv1alpha1.Environment) platformv1alpha1.Resources {
	out, ok := tierDefaults[env.Spec.Tier]
	if !ok {
		out = tierDefaults["dev"] // safest fallback; enum should prevent this
	}
	req := env.Spec.Resources

	if !req.CPU.IsZero() {
		out.CPU = req.CPU
	}
	if !req.Memory.IsZero() {
		out.Memory = req.Memory
	}
	if req.Pods > 0 {
		out.Pods = req.Pods
	}
	return out
}

// deregister removes this environment's entry. Must be safe to call repeatedly:
// it runs on every retry of a failed delete, and after a controller restart
// mid-deletion.
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

func (r *EnvironmentReconciler) ready(ctx context.Context, env *platformv1alpha1.Environment, nsName string) error {
	before := env.Status.DeepCopy()

	env.Status.Namespace = nsName
	env.Status.ObservedGeneration = env.Generation
	eff := effectiveResources(env)
	env.Status.EffectiveResources = &eff
	if ttl := effectiveTTL(env); ttl != nil {
		expiry := metav1.NewTime(env.CreationTimestamp.Add(ttl.Duration))
		env.Status.ExpiresAt = &expiry
	} else {
		env.Status.ExpiresAt = nil
	}
	meta.SetStatusCondition(&env.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "namespace, quota and network policy are in place",
		ObservedGeneration: env.Generation,
	})

	// Nothing changed, so don't write. A no-op write still costs a round trip,
	// still risks a conflict, and still wakes up everything watching this object.
	if equality.Semantic.DeepEqual(before, &env.Status) {
		return nil
	}
	return r.Status().Update(ctx, env)
}

func (r *EnvironmentReconciler) failed(ctx context.Context, env *platformv1alpha1.Environment, reason string, cause error) (ctrl.Result, error) {
	before := env.Status.DeepCopy()

	meta.SetStatusCondition(&env.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            cause.Error(),
		ObservedGeneration: env.Generation,
	})

	// Only write if the condition actually moved. A failure that repeats every
	// few seconds during backoff would otherwise write identical status each
	// time, and each write wakes up everything watching this object.
	if !equality.Semantic.DeepEqual(before, &env.Status) {
		// Deliberately discarding this error: if the status write fails we still
		// want to return the original cause. Losing it here makes the real
		// problem very hard to find later.
		_ = r.Status().Update(ctx, env)
	}

	return ctrl.Result{}, cause
}

const defaultTTL = 168 * time.Hour

func effectiveTTL(env *platformv1alpha1.Environment) *metav1.Duration {
	if env.Spec.Tier == "prod" {
		return nil
	}
	if env.Spec.TTL != nil {
		return env.Spec.TTL
	}
	return &metav1.Duration{Duration: defaultTTL}
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Environment{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("environment").
		Complete(r)
}
