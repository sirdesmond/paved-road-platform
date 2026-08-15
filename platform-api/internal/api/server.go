package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	pub "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

// Metrics tracks adoption: self-served vs expert-assisted provisioning.
// If teams don't use the paved road, the paved road is wrong — so counting
// this is part of the product, not an afterthought.
type Metrics struct {
	provisioned atomic.Int64
}

func (m *Metrics) Provisioned(source string) { m.provisioned.Add(1) }
func (m *Metrics) Count() int64              { return m.provisioned.Load() }

type Server struct {
	gh      *GitHub
	metrics *Metrics
	k8s     client.Client
}

// NewServer takes a Kubernetes client for the read endpoints. It may be nil:
// creating an environment only needs GitHub, so the API stays useful without
// cluster access and the read endpoints degrade rather than the whole service
// refusing to start.
func NewServer(gh *GitHub, k8s client.Client) *Server {
	return &Server{gh: gh, k8s: k8s, metrics: &Metrics{}}
}

// requireK8s guards the read paths. Returns false and writes a 503 if the
// server started without cluster access.
func (s *Server) requireK8s(w http.ResponseWriter) bool {
	if s.k8s == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "this endpoint needs cluster access and platform-api started without it. " +
				"Check its startup logs; create still works.",
		})
		return false
	}
	return true
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/environments", s.createEnvironment)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int64{"provisioned": s.metrics.Count()})
	})
	mux.HandleFunc("GET /v1/environments", s.listEnvironments)
	mux.HandleFunc("GET /v1/environments/{name}", s.environmentStatus)
	return mux
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var req pub.CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "malformed JSON: " + err.Error(),
		})
		return
	}

	if errs := Validate(req); len(errs) > 0 {
		// 422, not 400: the JSON was fine, the request wasn't allowed.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}

	env := ToEnvironment(req)

	// sigs.k8s.io/yaml, not gopkg.in/yaml — it marshals via JSON tags, so the
	// output matches the CRD. A plain YAML marshaller would emit Go field
	// names and produce a manifest the API server rejects.
	manifest, err := yaml.Marshal(env)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rendering manifest"})
		return
	}

	url, err := s.gh.OpenEnvironmentPR(r.Context(), env, manifest)
	if errors.Is(err, ErrAlreadyExists) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("%s already exists. To change it, edit %s in Git. "+
				"This API only creates.", env.Name, EnvironmentPath(env)),
		})
		return
	}
	if err != nil {
		// 502: we're fine, GitHub isn't. Distinct from 422 (your request was
		// wrong) and 409 (it already exists), because the caller's next action
		// differs in each case — fix the request, look at the existing one, or
		// retry in a minute.
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.metrics.Provisioned("api")

	// 202, not 201: the environment does not exist yet. A pull request does.
	writeJSON(w, http.StatusAccepted, pub.CreateEnvironmentResponse{
		Name:        env.Name,
		PullRequest: url,
		Status:      "pending-merge",
		Message:     "Merge the PR and Argo CD will provision it, usually within a minute",
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
