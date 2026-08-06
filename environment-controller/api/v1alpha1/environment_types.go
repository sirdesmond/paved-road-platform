package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Owner identifies the team responsible for this environment.
// Nothing exists in this platform without an owner.
type Owner struct {
	// Team is the owning team's identifier, used for labels and RBAC.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=40
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Team string `json:"team"`

	// Contact is where to reach the team: a Slack channel, a rota, an email.
	// +kubebuilder:validation:MinLength=1
	Contact string `json:"contact"`
}

// Resources is the ceiling for the environment's namespace.
type Resources struct {
	// CPU is the total CPU the namespace may request, e.g. "4" or "500m".
	CPU resource.Quantity `json:"cpu,omitempty"`

	// Memory is the total memory the namespace may request, e.g. "8Gi".
	Memory resource.Quantity `json:"memory,omitempty"`

	// Pods is the maximum number of pods in the namespace.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=200
	Pods int32 `json:"pods,omitempty"`
}

// EnvironmentSpec is the whole contract a team writes.
// Keep this small. Everything absent from here is a platform default,
// and defaults are where the safety lives.
// +kubebuilder:validation:XValidation:rule="self.tier != 'prod' || !has(self.ttl)",message="production environments do not expire: remove ttl, or use tier staging if you want it reaped"
type EnvironmentSpec struct {
	// Owner is mandatory. See the Owner type.
	Owner Owner `json:"owner"`

	// +kubebuilder:validation:Enum=dev;staging;prod
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="tier is immutable: create a new environment at the target tier and migrate to it"
	Tier string `json:"tier"`

	// Resources overrides the tier's default ceiling, field by field.
	// Deliberately no schema default: the API server would fill it in before
	// the controller ever saw the object, and we'd lose the ability to tell
	// "team didn't say" from "team asked for exactly this". Tier defaults are
	// logic, so they live in the controller. See status.effectiveResources for
	// what was actually applied.
	// +optional
	Resources Resources `json:"resources,omitempty"`

	// TTL is how long a non-prod environment lives before it's reaped.
	// +optional
	TTL *metav1.Duration `json:"ttl,omitempty"`
}

// EnvironmentStatus is written by the controller, never by a user.
type EnvironmentStatus struct {
	// Namespace is the namespace provisioned for this environment.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// ObservedGeneration is the .metadata.generation this status reflects.
	// Without it you can't tell "reconciled and healthy" from "not looked at yet".
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions follow the standard Kubernetes convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ExpiresAt is when this environment becomes eligible for reaping.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// EffectiveResources is the ceiling actually applied, after tier defaults
	// and any overrides. This is what the quota was built from.
	// +optional
	EffectiveResources *Resources `json:"effectiveResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=env
// +kubebuilder:printcolumn:name="Tier",type=string,JSONPath=`.spec.tier`
// +kubebuilder:printcolumn:name="Team",type=string,JSONPath=`.spec.owner.team`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Expires",type=string,JSONPath=`.status.expiresAt`

// Environment is a team's isolated slice of the platform.
type Environment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EnvironmentSpec   `json:"spec,omitempty"`
	Status EnvironmentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EnvironmentList contains a list of Environment.
type EnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Environment `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(SchemeGroupVersion, &Environment{}, &EnvironmentList{})
		return nil
	})
}
