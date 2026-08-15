package api

import (
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
	pub "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
	if !s.requireK8s(w) {
		return
	}

	var list v1alpha1.EnvironmentList
	if err := s.k8s.List(r.Context(), &list); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	out := make([]pub.EnvironmentSummary, 0, len(list.Items))
	for _, env := range list.Items {
		out = append(out, pub.EnvironmentSummary{
			Name:      env.Name,
			Team:      env.Spec.Owner.Team,
			Tier:      env.Spec.Tier,
			Namespace: env.Status.Namespace,
			Ready:     meta.IsStatusConditionTrue(env.Status.Conditions, "Ready"),
			ExpiresAt: formatTime(env.Status.ExpiresAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) environmentStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireK8s(w) {
		return
	}

	name := r.PathValue("name")

	var env v1alpha1.Environment
	err := s.k8s.Get(r.Context(), client.ObjectKey{Name: name}, &env)

	if apierrors.IsNotFound(err) {
		// Not in the cluster. The interesting case: is it waiting on a merge?
		// This is the difference between "your request is stuck" and "your
		// request never existed", and it's the question people actually ask.
		if url, prErr := s.gh.FindOpenPR(r.Context(), name); prErr == nil && url != "" {
			writeJSON(w, http.StatusOK, pub.EnvironmentStatus{
				Name:        name,
				Phase:       "pending-merge",
				PullRequest: url,
				Message:     "waiting for the pull request to be merged",
			})
			return
		}
		writeJSON(w, http.StatusNotFound, pub.EnvironmentStatus{
			Name:    name,
			Phase:   "unknown",
			Message: "no environment and no open pull request. Was it requested?",
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	st := pub.EnvironmentStatus{
		Name:      env.Name,
		Namespace: env.Status.Namespace,
		ExpiresAt: formatTime(env.Status.ExpiresAt),
		Phase:     "provisioning",
		Message:   "merged, waiting for the controller",
	}
	if cond := meta.FindStatusCondition(env.Status.Conditions, "Ready"); cond != nil {
		st.Message = cond.Message
		if cond.Status == "True" {
			st.Phase = "ready"
		} else {
			// Surface the reason. "NamespaceFailed" is far more useful than
			// "not ready", and it's already in the condition.
			st.Phase = "provisioning"
			st.Message = cond.Reason + ": " + cond.Message
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func formatTime(time *v1.Time) string {
	if time == nil {
		return ""
	}
	return time.Format("2006-01-02 15:04 MST")
}
