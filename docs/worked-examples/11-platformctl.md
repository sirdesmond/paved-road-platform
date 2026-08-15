# 11 — `platformctl`, the same contract from a terminal

**Mode:** worked
**Time:** about an hour
**You'll end up with:** a small CLI that creates environments through the API, with output people can read
and output machines can parse.

Assumes [10](./10-platform-api.md) is running.

---

## Files you'll create

```
paved-road-platform/
└── platformctl/                     # NEW module
    ├── go.mod
    └── main.go                      # all of it — this is deliberately one file
```

```bash
mkdir platformctl && cd platformctl
go mod init github.com/sirdesmond/paved-road-platform/platformctl
go mod edit -replace github.com/sirdesmond/paved-road-platform/platform-api=../platform-api
go get github.com/sirdesmond/paved-road-platform/platform-api
```

## Where read-truth lives

`list` and `status` need to read state, and there are two places to read it from: the API proxies it, or the
CLI talks to the cluster directly.

**The API proxies it.** If the platform's promise is that teams don't need cluster access, a CLI requiring
kubeconfig and RBAC breaks that promise for exactly the people it exists to serve. A developer should need a
URL and nothing else.

The cost is that `platform-api` gains a Kubernetes client, and in-cluster it needs a ServiceAccount that can
`get`/`list` environments. Worth it.

No Cobra. A few flags and three subcommands don't justify a dependency tree, and `flag` gets you `--help`
free.

## The one rule

**The CLI is a client. It renders input and output. All judgment lives behind the API.**

No tier ceilings here, no naming rules, no defaults. If `platformctl` can tell you a request is invalid
without asking the API, you now have two implementations of validation and they will drift.

That's why it imports the wire types from `platform-api` rather than declaring its own — a shared struct
can't disagree with itself.

## Part A: read endpoints in `platform-api`

**File: `platform-api/internal/api/k8s.go`**

```go
package api

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
)

// NewK8sClient reads from kubeconfig locally and the ServiceAccount in-cluster.
// In-cluster it needs get/list on environments — see the RBAC note below.
func NewK8sClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}
```

**File: `platform-api/pkg/api/types.go`** — the shared wire types, moved out of `internal/` so
`platformctl` can import them (see the troubleshooting note; Go enforces `internal/` at the module boundary).
Add these alongside the request and response types:

```go
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
	// Phase is the one thing a developer actually wants: where is my request?
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
```

**File: `platform-api/internal/api/read.go`**

```go
package api

import (
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
	pub "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
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
```

`formatTime` is a two-liner returning `""` for nil and RFC3339 otherwise.

**In `github.go`**, the open-PR lookup:

```go
func (g *GitHub) FindOpenPR(ctx context.Context, envName string) (string, error) {
	prs, _, err := g.client.PullRequests.List(ctx, g.owner, g.repo, &github.PullRequestListOptions{
		State: "open",
		Head:  g.owner + ":env/" + envName,
	})
	if err != nil || len(prs) == 0 {
		return "", err
	}
	return prs[0].GetHTMLURL(), nil
}
```

**Wire them up** in `Routes()`, and give `Server` a `k8s client.Client` field:

```go
	mux.HandleFunc("GET /v1/environments", s.listEnvironments)
	mux.HandleFunc("GET /v1/environments/{name}", s.environmentStatus)
```

`{name}` path variables need Go 1.22+, which you have.

**RBAC**, when this runs in-cluster rather than from your laptop: a ServiceAccount with `get`, `list` and
`watch` on `environments.platform.internal`. Locally it uses your kubeconfig and you won't notice — which is
the same trap as example 06, so expect it.

## Part B: the CLI

**File: `platformctl/main.go`**

```go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	api "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "env" {
		usage()
	}

	switch os.Args[2] {
	case "create":
		os.Exit(createCmd(os.Args[3:]))
	case "list":
		os.Exit(listCmd(os.Args[3:]))
	case "status":
		os.Exit(statusCmd(os.Args[3:]))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `platformctl — request and inspect environments

  platformctl env create --team NAME --tier dev|staging|prod --contact CONTACT
  platformctl env list
  platformctl env status NAME

An environment is an isolated namespace with a resource quota, a default-deny
network policy and an owner. Creating one opens a pull request; merging it
provisions the environment.

  --endpoint   platform-api URL (or $PLATFORM_API)
  -o           text | json
`)
	os.Exit(2)
}

func createCmd(args []string) int {
	fs := flag.NewFlagSet("env create", flag.ExitOnError)
	var req api.CreateEnvironmentRequest
	var endpoint, format string

	fs.StringVar(&req.Team, "team", "", "Owning team.")
	fs.StringVar(&req.Tier, "tier", "dev", "dev, staging or prod.")
	fs.StringVar(&req.Contact, "contact", "", "Slack channel or rota to page.")
	fs.StringVar(&req.Requester, "requester", os.Getenv("USER"), "Who is asking.")
	fs.StringVar(&req.Environment, "name", "", "Override the generated name.")
	fs.StringVar(&req.CPU, "cpu", "", "CPU ceiling, e.g. 2 or 500m. Omit for the tier default.")
	fs.StringVar(&req.Memory, "memory", "", "Memory ceiling, e.g. 4Gi. Omit for the tier default.")
	fs.StringVar(&endpoint, "endpoint", envOr("PLATFORM_API", "http://localhost:8080"), "platform-api base URL.")
	fs.StringVar(&format, "o", "text", "Output format: text or json.")
	_ = fs.Parse(args)

	return create(endpoint, format, req)
}

func listCmd(args []string) int {
	fs := flag.NewFlagSet("env list", flag.ExitOnError)
	endpoint := fs.String("endpoint", envOr("PLATFORM_API", "http://localhost:8080"), "platform-api base URL.")
	format := fs.String("o", "text", "Output format: text or json.")
	_ = fs.Parse(args)

	body, status, err := get(*endpoint + "/v1/environments")
	if err != nil {
		return unreachable(*endpoint, err)
	}
	if *format == "json" {
		fmt.Println(string(body))
		return exitFor(status)
	}

	var envs []api.EnvironmentSummary
	if err := json.Unmarshal(body, &envs); err != nil {
		fmt.Fprintf(os.Stderr, "unexpected response: %s\n", body)
		return 4
	}
	if len(envs) == 0 {
		fmt.Println("No environments yet. Create one with: platformctl env create --team NAME --tier dev")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTEAM\tTIER\tREADY\tEXPIRES")
	for _, e := range envs {
		ready := "no"
		if e.Ready {
			ready = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Team, e.Tier, ready, dash(e.ExpiresAt))
	}
	w.Flush()
	return 0
}

func statusCmd(args []string) int {
	fs := flag.NewFlagSet("env status", flag.ExitOnError)
	endpoint := fs.String("endpoint", envOr("PLATFORM_API", "http://localhost:8080"), "platform-api base URL.")
	format := fs.String("o", "text", "Output format: text or json.")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: platformctl env status NAME")
		return 2
	}
	name := fs.Arg(0)

	body, status, err := get(*endpoint + "/v1/environments/" + name)
	if err != nil {
		return unreachable(*endpoint, err)
	}
	if *format == "json" {
		fmt.Println(string(body))
		return exitFor(status)
	}

	var st api.EnvironmentStatus
	if err := json.Unmarshal(body, &st); err != nil {
		fmt.Fprintf(os.Stderr, "unexpected response: %s\n", body)
		return 4
	}

	// Lead with the phase. It's the answer to the question they asked.
	fmt.Printf("%s: %s\n", st.Name, st.Phase)
	if st.Message != "" {
		fmt.Printf("  %s\n", st.Message)
	}
	if st.PullRequest != "" {
		fmt.Printf("  %s\n", st.PullRequest)
	}
	if st.Namespace != "" {
		fmt.Printf("  namespace: %s\n", st.Namespace)
	}
	if st.ExpiresAt != "" {
		fmt.Printf("  expires:   %s\n", st.ExpiresAt)
	}
	return exitFor(status)
}

func get(url string) ([]byte, int, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return buf.Bytes(), resp.StatusCode, nil
}

func unreachable(endpoint string, err error) int {
	fmt.Fprintf(os.Stderr, "cannot reach platform-api at %s: %v\n", endpoint, err)
	return 3
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
```

And the `create` implementation the subcommand calls, plus the two shared helpers:

```go
func create(endpoint, format string, req api.CreateEnvironmentRequest) int {
	body, _ := json.Marshal(req)
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Post(endpoint+"/v1/environments", "application/json", bytes.NewReader(body))
	if err != nil {
		// Distinct exit code: "couldn't reach the platform" is a different
		// problem from "your request was wrong", and a pipeline needs to tell
		// them apart.
		fmt.Fprintf(os.Stderr, "cannot reach platform-api at %s: %v\n", endpoint, err)
		return 3
	}
	defer resp.Body.Close()

	if format == "json" {
		// Pass the API's response through untouched so scripts depend on the
		// API's contract, not on ours.
		var raw json.RawMessage
		_ = json.NewDecoder(resp.Body).Decode(&raw)
		fmt.Println(string(raw))
		return exitFor(resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusAccepted:
		var out api.CreateEnvironmentResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		fmt.Printf("%s requested\n  %s\n  %s\n", out.Name, out.PullRequest, out.Message)
		return 0

	case http.StatusUnprocessableEntity:
		var out struct {
			Errors []api.ValidationError `json:"errors"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		for _, e := range out.Errors {
			// Field first, then the explanation indented. Nobody reads a JSON
			// dump, and this is the whole perceived quality of the tool.
			fmt.Fprintf(os.Stderr, "✗ %s\n    %s\n", e.Field, e.Message)
		}
		return 1

	default:
		var out map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&out)
		fmt.Fprintf(os.Stderr, "✗ %s\n", out["error"])
		return exitFor(resp.StatusCode)
	}
}

func exitFor(status int) int {
	switch {
	case status < 300:
		return 0
	case status < 500:
		return 1 // your request
	default:
		return 4 // theirs
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

Four decisions worth noticing in that.

**`-o json` passes the API's response straight through** rather than re-marshalling a CLI-specific shape.
Scripts then depend on the API's contract, and you don't have a third schema to version.

**Exit codes distinguish causes.** `1` for a bad request, `3` for can't-reach-the-API, `4` for the API
failing. Someone will put this in CI within a week, and "did it fail because I was wrong or because the
platform is down" is the first question they'll ask.

**Validation errors render as field-then-explanation**, not a JSON blob. This is most of what makes a CLI feel
finished.

**`--requester` defaults to `$USER`** so nobody has to type it, while remaining an explicit field. Same
honesty caveat as the API: self-declared until there's auth.

---

## Checkpoint

```bash
go build -o platformctl .

./platformctl env create --team orders --tier dev --contact '#team-orders'
# → name, PR link, exit 0

./platformctl env status orders-dev
# → pending-merge, with the PR link. Merge it, then:
./platformctl env status orders-dev
# → provisioning, then ready with a namespace

./platformctl env list
# → a table of everything, including the ones created through Git directly

./platformctl env create --team Orders_Team --tier production --cpu 999 --contact ''
# → readable field errors, exit 1

./platformctl env create --team orders --tier dev --contact '#x' -o json | jq .
# → parseable

PLATFORM_API=http://localhost:9999 ./platformctl env create --team orders --tier dev --contact '#x'
# → connection error, exit 3
```

Then the test that matters:

```bash
grep -nE '"dev"|"prod"|[0-9]+ *CPU|regexp' main.go
```

Any hit means the contract has already split. The only tier strings in this file should be in the `--tier`
flag's help text.

## If it went wrong

| What you see | Usually means |
|---|---|
| `use of internal package not allowed` | `internal/` is only importable within its own module. Either move the shared types out of `internal/` in platform-api, or declare them in a small `pkg/api` package both can import. |
| `missing go.sum entry` | `go mod tidy` after the replace directive. |
| Errors print as `map[]` | You're decoding the 422 body into the wrong shape — it's `{"errors": [...]}`, not a bare array. |

That first row will very likely bite you. Go enforces `internal/` at the module boundary, so
`platform-api/internal/api` is invisible to `platformctl`. The fix is a two-minute move of
`CreateEnvironmentRequest`, `CreateEnvironmentResponse` and `ValidationError` into `platform-api/pkg/api`,
leaving the handlers and GitHub client where they are. Worth doing rather than copying the types — the whole
point is that they can't drift.

## What `status` is really doing

It answers "where is my request?", and that answer lives in more than one system. The version here covers
three of the four states:

| Phase | Where the truth is | How we get it |
|---|---|---|
| `pending-merge` | GitHub | open PR on branch `env/<name>` |
| `provisioning` | the cluster | Environment exists, `Ready` condition false |
| `ready` | the cluster | `Ready` condition true |
| *merged but not synced* | Argo CD | **not covered** — looks like `unknown` for a minute |

That gap is worth knowing rather than hiding: between the merge and Argo CD's next sync, the PR is closed and
the object doesn't exist yet, so `status` reports `unknown`. Closing it means querying the Argo CD API for the
Application's sync status — a fourth system, and a fair amount of work for a one-minute window.

Note also the failure-reason passthrough in `environmentStatus`. When the controller sets `Ready=False` with
reason `NamespaceFailed`, the CLI prints that reason rather than "not ready". That one line is the difference
between a developer self-serving a fix and a developer opening a ticket — which is the entire premise of the
platform, showing up in an error string.

Next: 12 — Terraform, and the cloud foundations.
