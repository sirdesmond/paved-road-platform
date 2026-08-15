// Package api is the platform's public wire contract: the types clients send
// and receive. Deliberately types only — no validation, no rendering, no
// judgment. Those live in internal/, so a client can speak the protocol but
// cannot reimplement the rules. See ARCHITECTURE §4.
package api

// CreateEnvironmentRequest is simpler than the Environment CRD on purpose.
// Callers shouldn't need to know about apiVersion or metadata.
type CreateEnvironmentRequest struct {
	Team        string `json:"team"`
	Tier        string `json:"tier"`
	Environment string `json:"environment,omitempty"` // defaults to "<team>-<tier>"
	CPU         string `json:"cpu,omitempty"`
	Memory      string `json:"memory,omitempty"`
	Pods        int32  `json:"pods,omitempty"`
	Contact     string `json:"contact"`

	// Requester is who asked. SELF-DECLARED — the API is unauthenticated, so
	// treat it as provenance, not identity.
	Requester string `json:"requester"`
}

type CreateEnvironmentResponse struct {
	Name        string `json:"name"`
	PullRequest string `json:"pullRequest"`
	Status      string `json:"status"` // "pending-merge"
	Message     string `json:"message"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type EnvironmentSummary struct {
	Name      string `json:"name"`
	Team      string `json:"team"`
	Tier      string `json:"tier"`
	Namespace string `json:"namespace,omitempty"`
	Ready     bool   `json:"ready"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type EnvironmentStatus struct {
	Name string `json:"name"`
	// Phase answers the only question a developer is asking:
	//   pending-merge  — a PR is open, nobody has merged it
	//   provisioning   — merged and in the cluster, controller hasn't finished
	//   ready          — usable
	//   unknown        — not in the cluster and no open PR found
	Phase       string `json:"phase"`
	Namespace   string `json:"namespace,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
	Message     string `json:"message,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}
