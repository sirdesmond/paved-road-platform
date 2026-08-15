package api

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
	pub "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

const (
	// AnnotationRequestedBy is self-declared until the API has auth. Labelled
	// as such deliberately: an audit trail you've forgotten is unverified is
	// worse than no audit trail.
	AnnotationRequestedBy  = "platform.internal/requested-by"
	AnnotationRequestedVia = "platform.internal/requested-via"
)

// ToEnvironment renders the request as the CRD the controller reconciles.
//
// Note what this does NOT do: fill in defaults. If it wrote 2 CPUs here, that
// would be recorded as an explicit request and the team could never inherit a
// changed tier default. Defaults belong in the controller (ADR-0006).
func ToEnvironment(r pub.CreateEnvironmentRequest) *v1alpha1.Environment {
	name := r.Environment
	if name == "" {
		name = fmt.Sprintf("%s-%s", r.Team, r.Tier)
	}

	env := &v1alpha1.Environment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "platform.internal/v1alpha1",
			Kind:       "Environment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			// Provenance as an annotation, not a spec field: it records how the
			// object came to exist, which isn't something the team is asking
			// for. It also survives into the cluster, so `kubectl get env -o yaml`
			// answers "who wanted this" long after the PR is forgotten.
			Annotations: map[string]string{
				AnnotationRequestedBy:  r.Requester,
				AnnotationRequestedVia: "platform-api",
			},
		},
		Spec: v1alpha1.EnvironmentSpec{
			Owner: v1alpha1.Owner{Team: r.Team, Contact: r.Contact},
			Tier:  r.Tier,
		},
	}

	if r.CPU != "" {
		env.Spec.Resources.CPU = resource.MustParse(r.CPU)
	}
	if r.Memory != "" {
		env.Spec.Resources.Memory = resource.MustParse(r.Memory)
	}
	if r.Pods > 0 {
		env.Spec.Resources.Pods = r.Pods
	}
	return env
}
