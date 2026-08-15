# 10 — `platform-api`: validate, render, open a PR

**Mode:** worked
**Time:** three to four hours
**You'll end up with:** a Go service that turns `POST /v1/environments` into a pull request, with validation
that produces error messages a person can act on.

Assumes Part 1. Doesn't need Part 2, though the PR it opens is what Argo CD will sync.

---

## Files you'll create

A new Go module alongside the controller, at the repo root:

```
paved-road-platform/
├── environment-controller/          (existing — imported as a dependency)
└── platform-api/                    # NEW module
    ├── go.mod
    ├── cmd/platform-api/main.go     # NEW: flags, wiring, mux
    └── internal/api/
        ├── request.go               # NEW: CreateEnvironmentRequest + Validate()
        ├── render.go                # NEW: ToEnvironment()
        ├── github.go                # NEW: OpenEnvironmentPR()
        └── server.go                # NEW: the HTTP handler
```

Code blocks below say `package api` — those go under `internal/api/`. Split across the files above or keep
them in one; the layout matters less than the boundary between validation, rendering and Git.

Not deployed by Argo CD in this example. Run it locally against your own GitHub token first; packaging it as
a platform component is a follow-up once it does something useful.

## What this is for

[RFC-0001](../rfc/0001-self-service-environments.md) argued that self-service shouldn't mean giving up the
audit trail: a request becomes a pull request, Git stays the only write path, and non-prod auto-merges so the
PR is a record rather than a queue.

There's a second reason that only became obvious later. The
[ADR-0004 amendment](../adr/0004-policy-enforcement-layers.md#amendment-2026-08-09-git-is-also-a-request-path)
noted that request-time validation is where the *useful* error message lives — "your team's budget is 32 CPUs
and you asked for 64" beats anything admission control can say. This service is where that check goes.

## The contract, and how to keep one

The API takes a request, but what it *produces* is an `Environment` — the same type the controller reconciles.
So import it rather than redeclaring it:

```bash
mkdir platform-api && cd platform-api
go mod init github.com/sirdesmond/paved-road-platform/platform-api
go get github.com/sirdesmond/paved-road-platform/environment-controller@latest
go get github.com/google/go-github/v66/github
go get sigs.k8s.io/yaml
```

That import is the design decision. Three surfaces (API, controller, CLI) and one type: the API physically
cannot render an `Environment` the controller won't understand, because it's the same struct. If you copy the
type instead, they drift the first time someone adds a field.

If `go get` on the sibling module gives you trouble before it's tagged, a `replace` directive in `go.mod`
pointing at `../environment-controller` works fine for local development.

## Step 1: the request type

Deliberately *not* the CRD. The wire format is the platform's public API and should be simpler than the
Kubernetes object — no `apiVersion`, no `metadata`, nothing a caller shouldn't have to know.

```go
package api

type CreateEnvironmentRequest struct {
	Team        string `json:"team"`
	Tier        string `json:"tier"`
	Environment string `json:"environment,omitempty"` // defaults to "<team>-<tier>"
	CPU         string `json:"cpu,omitempty"`
	Memory      string `json:"memory,omitempty"`
	Pods        int32  `json:"pods,omitempty"`
	Contact     string `json:"contact"`
}

type CreateEnvironmentResponse struct {
	Name      string `json:"name"`
	PullRequest string `json:"pullRequest"`
	Status    string `json:"status"`  // "pending-merge"
	Message   string `json:"message"`
}
```

## Step 2: validation that explains itself

This is the part worth caring about. Every message should tell the caller what to do next.

```go
package api

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// tierMax is the platform's ceiling per tier. Requests above it need a
// conversation, not a bigger number.
var tierMax = map[string]struct {
	CPU  string
	Pods int32
}{
	"dev":     {CPU: "8", Pods: 20},
	"staging": {CPU: "16", Pods: 50},
	"prod":    {CPU: "64", Pods: 200},
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (r CreateEnvironmentRequest) Validate() []ValidationError {
	var errs []ValidationError

	if !nameRE.MatchString(r.Team) {
		errs = append(errs, ValidationError{
			Field: "team",
			Message: fmt.Sprintf("%q must be lowercase letters, numbers and hyphens "+
				"(it becomes a namespace label and must be DNS-safe)", r.Team),
		})
	}

	if strings.TrimSpace(r.Contact) == "" {
		errs = append(errs, ValidationError{
			Field:   "contact",
			Message: "required: a Slack channel or rota, so we know who to page. Environments without an owner get reaped",
		})
	}

	max, ok := tierMax[r.Tier]
	if !ok {
		errs = append(errs, ValidationError{
			Field:   "tier",
			Message: fmt.Sprintf("%q is not a tier. Use dev, staging or prod", r.Tier),
		})
		return errs // no point checking quotas against an unknown tier
	}

	if r.CPU != "" {
		want, err := resource.ParseQuantity(r.CPU)
		if err != nil {
			errs = append(errs, ValidationError{
				Field:   "cpu",
				Message: fmt.Sprintf("%q isn't a quantity. Try \"2\" or \"500m\"", r.CPU),
			})
		} else if limit := resource.MustParse(max.CPU); want.Cmp(limit) > 0 {
			errs = append(errs, ValidationError{
				Field: "cpu",
				Message: fmt.Sprintf("%s exceeds the %s ceiling of %s. "+
					"Ask in #platform if you genuinely need more — we'd rather raise it deliberately than have you split into two environments",
					r.CPU, r.Tier, max.CPU),
			})
		}
	}

	return errs
}
```

Read those messages as the developer receiving them. That's the actual product here — the YAML is
incidental.

## Step 3: render the Environment

```go
func (r CreateEnvironmentRequest) ToEnvironment() *v1alpha1.Environment {
	name := r.Environment
	if name == "" {
		name = fmt.Sprintf("%s-%s", r.Team, r.Tier)
	}

	env := &v1alpha1.Environment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "platform.internal/v1alpha1",
			Kind:       "Environment",
		},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.EnvironmentSpec{
			Owner: v1alpha1.Owner{Team: r.Team, Contact: r.Contact},
			Tier:  r.Tier,
		},
	}

	// Only set what was asked for. Anything omitted stays absent so the
	// controller's tier defaults apply — see ADR-0006.
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
```

Note the restraint: the API does *not* fill in defaults. If it did, the rendered spec would record 2 CPUs as
an explicit request, and the team could never inherit a changed tier default. Same reasoning as ADR-0006, one
layer up.

`sigs.k8s.io/yaml` marshals it — it goes via JSON tags, so the output matches the CRD exactly.

## Step 4: open the pull request

No local clone needed; four GitHub API calls.

```go
func (g *GitHub) OpenEnvironmentPR(ctx context.Context, env *v1alpha1.Environment, manifest []byte) (string, error) {
	branch := "env/" + env.Name
	path := fmt.Sprintf("environments/%s/environment.yaml", env.Spec.Owner.Team)

	// 1. where main is now
	base, _, err := g.client.Git.GetRef(ctx, g.owner, g.repo, "refs/heads/"+g.baseBranch)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", g.baseBranch, err)
	}

	// 2. a branch off it — tolerate it already existing, so a retried
	//    request doesn't fail with a confusing 422
	_, _, err = g.client.Git.CreateRef(ctx, g.owner, g.repo, &github.Reference{
		Ref:    github.String("refs/heads/" + branch),
		Object: &github.GitObject{SHA: base.Object.SHA},
	})
	if err != nil && !strings.Contains(err.Error(), "Reference already exists") {
		return "", fmt.Errorf("creating branch %s: %w", branch, err)
	}

	// 3. the file
	_, _, err = g.client.Repositories.CreateFile(ctx, g.owner, g.repo, path, &github.RepositoryContentFileOptions{
		Message: github.String(fmt.Sprintf("Add environment %s for %s", env.Name, env.Spec.Owner.Team)),
		Content: manifest,
		Branch:  github.String(branch),
	})
	if err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}

	// 4. the PR
	pr, _, err := g.client.PullRequests.Create(ctx, g.owner, g.repo, &github.NewPullRequest{
		Title: github.String(fmt.Sprintf("Environment: %s (%s)", env.Name, env.Spec.Tier)),
		Head:  github.String(branch),
		Base:  github.String(g.baseBranch),
		Body: github.String(fmt.Sprintf(
			"Requested via platform-api.\n\n- Team: %s\n- Tier: %s\n- Contact: %s\n",
			env.Spec.Owner.Team, env.Spec.Tier, env.Spec.Owner.Contact)),
	})
	if err != nil {
		return "", fmt.Errorf("opening PR: %w", err)
	}
	return pr.GetHTMLURL(), nil
}
```

The token comes from the environment, never from a file in the repo:

```go
ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")})
client := github.NewClient(oauth2.NewClient(ctx, ts))
```

Use a fine-grained PAT scoped to this one repository with contents and pull-requests write. Nothing else.

## Step 5: the handler

**File: `internal/api/server.go`** — along with the `Server` type it's a method on, the `Metrics` counter,
`writeJSON`, and the `Routes()` method below. Keeping the handler and its routing in one file means you can
see every endpoint the service exposes without opening `main.go`.


```go
func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var req api.CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed JSON: " + err.Error()})
		return
	}

	if errs := req.Validate(); len(errs) > 0 {
		// 422, not 400: the JSON was fine, the request wasn't allowed.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}

	env := req.ToEnvironment()
	manifest, err := yaml.Marshal(env)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rendering manifest"})
		return
	}

	url, err := s.gh.OpenEnvironmentPR(r.Context(), env, manifest)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.metrics.Provisioned("api") // adoption: self-served vs assisted
	writeJSON(w, http.StatusAccepted, api.CreateEnvironmentResponse{
		Name:        env.Name,
		PullRequest: url,
		Status:      "pending-merge",
		Message:     "Merge the PR and Argo CD will provision it, usually within a minute",
	})
}
```

Go 1.22's `net/http` mux handles method-aware routing; no framework required. This is a method on `Server`
in the same file:

```go
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/environments", s.createEnvironment)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	return mux
}
```

`cmd/platform-api/main.go` then does nothing but parse flags, read `GITHUB_TOKEN`, construct the server and
call `http.ListenAndServe(addr, srv.Routes())`. Keeping `main` thin means the interesting code is testable
without starting a process.

`202 Accepted` is the honest status code. The environment does not exist yet — a PR does.

---

## Checkpoint

```bash
export GITHUB_TOKEN=github_pat_...
go run ./cmd/platform-api
```

The unhappy path first, because that's the one you're building for:

```bash
curl -s -X POST localhost:8080/v1/environments \
  -H 'content-type: application/json' \
  -d '{"team":"Billing_Team","tier":"production","cpu":"999","contact":""}' | jq
```

Three errors, each naming the field and what to do. **Read them as though you were the developer.** If any
message would leave you guessing, rewrite it — that's the deliverable.

Then the happy path:

```bash
curl -s -X POST localhost:8080/v1/environments \
  -H 'content-type: application/json' \
  -d '{"team":"billing","tier":"dev","contact":"#team-billing"}' | jq
```

You should get a PR link. Open it, check the rendered YAML is a valid `Environment` with no defaults baked
in, merge it, and watch Argo CD provision it.

Run the same request twice. The second should behave sensibly rather than erroring on an existing branch —
which is the idempotency question the code above only half answers.

## If it went wrong

| What you see | Usually means |
|---|---|
| `401 Bad credentials` | Token not exported, or fine-grained PAT missing contents/pull-requests write. |
| `422 Reference already exists` | Retried request, same branch. Handled above — but check what your code does on a *second* file write to the same branch. |
| PR opens with an empty file | `Content` takes raw bytes; go-github base64-encodes it for you. Double-encoding gives you an empty or garbled file. |
| Rendered YAML has `creationTimestamp: null` | `metav1.ObjectMeta` marshals it. Harmless, but strip it if you want clean diffs — this is why some platforms render from templates instead of structs. |
| Argo CD doesn't pick up the merged PR | Path must match the ApplicationSet's `environments/*` glob. |

## Reflection

1. Two engineers on the same team request an environment at the same time. What happens, and what should?
2. The API is unauthenticated. Who is allowed to request a prod environment, and where does that check belong — the API, the PR review, or both?
3. Validation now lives in this service *and* in CEL on the CRD. Is that duplication a problem? What breaks if they disagree, and which one wins?
4. You've built the request path. A team can still bypass it entirely by opening the PR by hand. Is that a bug or a feature?

That last one is the whole philosophy of the platform in one question.

Next: 11 — `platformctl`, the same contract from a terminal.
