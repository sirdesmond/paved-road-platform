# 01 — Scaffold and the Environment API

**Mode:** worked (everything given, everything explained)
**Time:** about an hour if you read rather than paste
**You'll end up with:** a cluster-scoped `Environment` CRD installed on your kind cluster, with real
validation, sensible defaults and useful `kubectl get` output.

No reconciler yet. That's example 02. This one is about getting the *API* right, which matters more than it
sounds: the CRD is the contract every team will use, and it's the thing that's painful to change later once
people depend on it.

---

## Step 1: scaffold the project

```bash
mkdir environment-controller && cd environment-controller
go mod init github.com/YOURNAME/environment-controller

kubebuilder init \
  --domain internal \
  --repo github.com/YOURNAME/environment-controller
```

`--domain internal` is what makes the API group `platform.internal` later, matching the spec in
[RFC-0001](../rfc/0001-self-service-environments.md). Use a domain you actually control if this were real;
`internal` is fine for a platform that never leaves your clusters.

Have a look at what it generated. `cmd/main.go` sets up the manager, `config/` holds the Kustomize manifests,
and the `Makefile` has the targets you'll live in for the next few examples.

## Step 2: create the API

```bash
kubebuilder create api \
  --group platform \
  --version v1alpha1 \
  --kind Environment \
  --resource --controller \
  --namespaced=false
```

Say yes to both prompts if it asks.

**`--namespaced=false` is the important flag here.** An `Environment` creates a namespace, and a namespace is
a cluster-scoped object. A namespaced resource can't own a cluster-scoped one, so if you get this wrong the
garbage collector silently refuses to clean up after you and you'll spend an hour in example 03 wondering why
deletes don't work.

`v1alpha1` because this API *will* change. Starting at `v1` is a promise you can't keep yet.

## Step 3: define the spec

Open `api/v1alpha1/environment_types.go` and replace the generated `EnvironmentSpec`, `EnvironmentStatus`
and the type markers with this:

```go
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
	// per-field defaults below apply. See "Why it looks like that".
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
```

Leave `groupversion_info.go` alone, it's already correct. But do open it, because the shape of `init()` above
depends on what's in it.

Recent kubebuilder scaffolds declare `SchemeBuilder = runtime.NewSchemeBuilder(...)`, which is a
`runtime.SchemeBuilder`: a slice of `func(*runtime.Scheme) error`. So `Register` takes *functions*, which is
why the code above wraps `AddKnownTypes` in one.

Older scaffolds used controller-runtime's `&scheme.Builder{...}`, whose `Register` takes objects directly. If
your `groupversion_info.go` has that, use the shorter form instead:

```go
func init() {
	SchemeBuilder.Register(&Environment{}, &EnvironmentList{})
}
```

Getting this wrong gives you `cannot use &Environment{} ... as func(*Scheme) error value`, which reads like a
problem with your types but is really just the two scaffold styles. Match whatever your generated file uses.

## Why it looks like that

Worth reading, because these are the decisions someone will ask you about.

**The spec is deliberately tiny.** Four things a team can set: who owns it, which tier, and optionally CPU,
memory and pods. Everything else (network policy, monitoring, rollout strategy, TLS) is a tier default. Every
field you add is a field you support forever and a decision you've pushed onto someone who didn't want to make
it. When someone asks for a new field, the first question is whether it should be a tier instead.

**`Owner` is not optional.** Unowned resources are how platforms rot: nobody knows who to ask, so nobody
deletes anything. Making it a required field with a pattern means it can't be skipped or fudged, and the
`Team` value becomes a label you can group cost and alerts by later.

**`resource.Quantity` rather than `string`.** Quantity parses and validates Kubernetes' unit syntax for you,
so `"500m"` and `"8Gi"` work and `"eight gigs"` is rejected by the API server rather than by your reconciler
at 2am. It's also what you'll hand straight to a `ResourceQuota` in the next example, with no conversion.

**Validation markers do the work the API layer would otherwise repeat.** The enum on `Tier`, the pattern on
`Team`, the min and max on `Pods` are all enforced by the API server before your controller sees the object.
This is the "structural rules at admission" layer from
[ADR-0004](../adr/0004-policy-enforcement-layers.md), and you get it free from the CRD schema.

**Defaults don't cascade into absent objects.** This one catches nearly everyone. The API server applies
defaults to fields *inside* an object only when that object is present. Omit `resources` altogether and it
never looks inside, so `cpu`, `memory` and `pods` stay empty no matter what markers you put on them. The
`+kubebuilder:default={}` on the parent is what makes the whole thing work: absent becomes `{}`, and then the
per-field defaults populate it.

Worth internalising, because the same rule bites on every nested optional object you add later, and the
failure is silent. Nothing errors. You just get an empty field and a puzzled twenty minutes.

**`ObservedGeneration` matters more than it looks.** Without it, a status saying `Ready: True` might be
describing a version of the spec from ten minutes ago. With it, you can tell "reconciled and fine" apart from
"hasn't been looked at since you changed it", and so can anything reading the status.

**`+listType=map` on conditions** tells the API server these merge by `type` rather than being replaced
wholesale. Skip it and two controllers writing different conditions will clobber each other.

**Printer columns** are the smallest, highest-return thing in this file. `kubectl get env` becomes something a
person can read at a glance instead of a list of names, and it's the first thing anyone will use when
debugging.

## Step 4: generate and install

```bash
make generate   # deepcopy functions
make manifests  # the CRD yaml, from your markers
```

Look at what came out before you install it:

```bash
cat config/crd/bases/platform.internal_environments.yaml | head -40
```

That schema was generated entirely from the markers you wrote. Then install it:

```bash
make install
```

## Step 5: try it

Replace `config/samples/platform_v1alpha1_environment.yaml` with:

```yaml
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata:
  name: checkout-staging
spec:
  owner:
    team: checkout
    contact: "#team-checkout"
  tier: staging
  resources:
    cpu: "8"
    memory: 16Gi
    pods: 30
```

```bash
kubectl apply -f config/samples/platform_v1alpha1_environment.yaml
```

---

## Checkpoint

Run these. If any of them don't match, fix it before moving on.

```bash
kubectl get environments
```

You should get a table with Tier, Team, Namespace, Ready and Age columns. Namespace and Ready will be empty,
since nothing is reconciling yet. That's expected.

```bash
kubectl get env checkout-staging -o jsonpath='{.spec.resources.cpu}{"\n"}'
```

Should print `8`.

Now check the defaults actually applied. Create one that omits `resources` entirely:

```bash
kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata:
  name: search-dev
spec:
  owner:
    team: search
    contact: "#team-search"
  tier: dev
EOF

kubectl get env search-dev -o jsonpath='{.spec.resources}{"\n"}'
```

Should print the defaults: cpu `4`, memory `8Gi`, pods `20`. If it prints `{}`, your defaults aren't in the
generated CRD, so re-run `make manifests && make install`.

Finally, confirm the validation rejects bad input:

```bash
kubectl apply -f - <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata:
  name: bad-one
spec:
  owner:
    team: Checkout_Team
    contact: "#x"
  tier: production
EOF
```

This should fail with two complaints: `tier` must be one of dev, staging, prod, and `team` doesn't match the
pattern. **Read the error message.** That message is the developer experience of your platform, and it's
worth caring that it's legible.

Commit:

```bash
git add -A && git commit -m "Environment API: cluster-scoped CRD with validation and defaults"
```

## If it went wrong

| What you see | Usually means |
|---|---|
| `no matches for kind "Environment"` | The CRD isn't installed. Run `make install`, check `kubectl get crd \| grep environments`. |
| Defaults don't apply | Two causes. Either you edited markers without regenerating (`make manifests && make install`), or the parent object is missing `+kubebuilder:default={}` so the API server never descends into it. Check with: `kubectl get crd environments.platform.internal -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.resources.default}'` — you want `{}`. Also note defaults apply on write, so existing objects won't backfill; re-apply them. |
| `unknown field "spec.resources.cpu"` | The generated CRD is stale, same fix as above. |
| Compile errors about `DeepCopy` | You changed types without running `make generate`. |
| `cannot use &Environment{} ... as func(*Scheme) error` | Your scaffold uses `runtime.NewSchemeBuilder`, so `Register` takes functions, not objects. See the note under step 3. |
| `metadata.annotations: Too long` on install | You've hit the client-side apply limit. `kubectl apply --server-side -f config/crd/bases/`. This is the same failure documented in the sibling repo's runbook, and you'll hit it again with bigger CRDs. |

---

## Your turn (faded)

Two changes, no code given.

**1. Add a TTL.** Non-prod environments should expire so the cluster doesn't fill up with abandoned ones. Add
an optional `ttl` field to the spec taking a duration-ish value, defaulting to `168h` for dev and staging.

Two things to work out: what type do you use for a duration in a CRD (there's a standard answer, and
`time.Duration` isn't it), and can a `+kubebuilder:default` marker vary by tier? If not, where does that logic
have to live instead, and what does that tell you about the difference between schema defaults and policy?

**2. Surface it.** Add a printer column so `kubectl get env` shows when each one expires.

Checkpoint: `kubectl get env` shows the new column, a new dev environment gets the default TTL, and a garbage
value like `"7 days"` is rejected by the API server rather than accepted.

## Before you move on

Write down why the resource is cluster-scoped while the reasoning is fresh. You'll be asked about it, and
recording decisions as you make them is the habit this whole repo is trying to build.

Worked version: [ADR-0005](../adr/0005-environment-is-cluster-scoped.md). Try writing yours before reading
it — the interesting part isn't the decision, it's spotting that the RBAC you give up is a real cost rather
than an afterthought.

Next: [02 — the reconciler](./02-reconciler.md), where this CRD starts actually making things.
