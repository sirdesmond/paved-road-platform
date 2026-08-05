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
	// +kubebuilder:default="4"
	CPU resource.Quantity `json:"cpu,omitempty"`

	// Memory is the total memory the namespace may request, e.g. "8Gi".
	// +kubebuilder:default="8Gi"
	Memory resource.Quantity `json:"memory,omitempty"`

	// Pods is the maximum number of pods in the namespace.
	// +kubebuilder:default=20
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=200
	Pods int32 `json:"pods,omitempty"`
}

// EnvironmentSpec is the whole contract a team writes.
// Keep this small. Everything absent from here is a platform default,
// and defaults are where the safety lives.
type EnvironmentSpec struct {
	// Owner is mandatory. See the Owner type.
	Owner Owner `json:"owner"`

	// Tier drives the defaults: quotas, rollout strategy, whether merges are automatic.
	// +kubebuilder:validation:Enum=dev;staging;prod
	Tier string `json:"tier"`

	// Resources overrides the tier's default ceiling.
	// The empty-object default is load-bearing: without it, omitting `resources`
	// means the API server never descends into this object, so none of the
	// per-field defaults (cpu, memory, pods) apply.
	// +optional
	// +kubebuilder:default={}
	Resources Resources `json:"resources,omitempty"`
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=env
// +kubebuilder:printcolumn:name="Tier",type=string,JSONPath=`.spec.tier`
// +kubebuilder:printcolumn:name="Team",type=string,JSONPath=`.spec.owner.team`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

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
