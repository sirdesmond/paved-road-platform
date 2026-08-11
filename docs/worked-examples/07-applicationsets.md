# 07 — ApplicationSets, and a region as a variable change

**Mode:** faded
**Time:** two hours
**You'll end up with:** platform components fanned out by a generator rather than copy-pasted, and team
environments that appear in Argo CD by adding a directory to Git.

Assumes [06](./06-argocd-app-of-apps.md) is green.

---

## Files you'll create

All at the repo root. Nothing is applied by hand — `platform-root` from example 06 already watches
`gitops/platform/`.

```
paved-road-platform/
├── gitops/platform/
│   ├── platform-components.yaml    # RENAMED from environment-controller.yaml, now an ApplicationSet
│   └── team-environments.yaml      # NEW: the git-directory generator
└── environments/                   # NEW: one directory per team
    ├── payments/environment.yaml   # NEW
    └── search/environment.yaml     # NEW
```

Renaming the file is safe — Argo CD tracks resources by name and kind, not filename. Changing
`metadata.name` inside would create a new resource and orphan the old one.

## The problem with what you have

`gitops/platform/environment-controller.yaml` hardcodes one repo, one path, one destination. Adding a second
region means copying that file and editing three fields. Adding a third means doing it again, and now a change
to the sync policy has to be made in three places, where it will drift.

That's the gap between "we have GitOps" and the goal on the roadmap: **a new region should be a variable
change, not a project.** If adding a region means writing manifests, you haven't got there.

## How ApplicationSets work

An `ApplicationSet` is a **generator** plus a **template**. The generator produces a list of parameter sets;
the template is rendered once per set, producing one `Application` each. A controller keeps them in sync — add
an entry, an Application appears; remove one, it's pruned.

The generators worth knowing:

- **list** — parameters you write out explicitly. Good for a small, deliberate set like regions.
- **git (directories)** — one parameter set per directory in a repo. Good for "one Application per team", because the team creates the directory.
- **git (files)** — one per matching config file, when you need richer parameters than a directory name.
- **cluster** — one per cluster registered with Argo CD, optionally filtered by label. The real answer for multi-region once you have real clusters.
- **matrix / merge** — combine two generators, e.g. every component across every region.

## Step 1: regions via a list generator (worked)

Replace `gitops/platform/environment-controller.yaml` with `gitops/platform/platform-components.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: platform-components
  namespace: argocd
spec:
  goTemplate: true
  # Fail loudly on a typo'd field instead of rendering an empty string into
  # something important. Worth having on from day one.
  goTemplateOptions: ["missingkey=error"]

  generators:
    - list:
        elements:
          - region: local
            server: https://kubernetes.default.svc
          # Adding a region is adding four lines here. That's the whole point.
          # - region: eu-west-1
          #   server: https://eks-eu-west-1.example.com

  template:
    metadata:
      name: 'environment-controller-{{.region}}'
    spec:
      project: default
      source:
        repoURL: https://github.com/sirdesmond/paved-road-platform.git
        targetRevision: main
        path: environment-controller/config/default
      destination:
        server: '{{.server}}'
        namespace: environment-controller-system
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
        syncOptions:
          - CreateNamespace=true
          - ServerSideApply=true
```

Apply it by pushing — `platform-root` already watches `gitops/platform/`, so the ApplicationSet arrives the
same way everything else does.

```bash
git add gitops/platform && git rm gitops/platform/environment-controller.yaml
git commit -m "Fan out platform components by region" && git push
```

Then watch `kubectl -n argocd get applicationsets,applications`.

Note what didn't change: the template. Sync policy, path and namespace are written once. Adding a region
touches only the generator, which is the property the roadmap is actually asking for.

**Locally both regions would point at the same cluster**, since you have one. That's fine for seeing the
mechanism — but be honest in your notes that you demonstrated the *pattern*, not multi-region itself. See the
stretch section.

## Step 2: team environments via a git generator (your turn)

Now the self-service half. A team should be able to add their environment to the platform without anyone on
the platform team touching a manifest.

Create the layout:

```
environments/
  payments/
    environment.yaml      # an Environment CR, tier: dev
  search/
    environment.yaml
```

Then write `gitops/platform/team-environments.yaml`: an ApplicationSet whose git directory generator produces
one Application per directory under `environments/`.

```yaml
  generators:
    - git:
        repoURL: https://github.com/sirdesmond/paved-road-platform.git
        revision: main
        directories:
          - path: environments/*
```

Each element gives you `{{.path.basename}}` (the team name) and `{{.path.path}}` (the full path). Use them for
the Application name and source path.

TODOs:

- **Name them so two teams can't collide**, and so it's obvious which team an Application belongs to in the UI.
- **Decide the destination namespace.** Careful: an `Environment` is cluster-scoped, so what does `destination.namespace` even mean here? Work out what it's doing before you set it.
- **Decide the sync policy.** Should a team's environment self-heal? Should it prune? Think about what pruning means when the resource in question is the thing that owns a namespace full of workloads.
- **Add `preserveResourcesOnDeletion: true`** to the ApplicationSet spec and work out what it protects you from. The answer is in the warning below.

## The warning: delete blast radius, again

Deleting an `ApplicationSet` deletes every `Application` it generated. If those Applications have the
resources finalizer, that cascades into everything they deployed.

So a single `git rm gitops/platform/team-environments.yaml` could, with automated pruning on, delete every
team's `Environment` — and each of those owns a namespace, so it takes their workloads with it. One file
removal, the whole estate.

`preserveResourcesOnDeletion: true` breaks that chain: removing the ApplicationSet (or an element from it)
leaves the deployed resources running and orphaned. You then clean them up deliberately.

This is the same lesson as prune in example 06, one level higher and with a bigger radius. The general shape:
**every layer of automation multiplies the consequences of deleting one file.** Worth knowing exactly what
each layer deletes before you rely on it.

---

## Checkpoint

**A region is a variable change:**

```bash
# uncomment the second element in the list generator, then
git commit -am "Add a second region" && git push
kubectl -n argocd get applications
```

A second Application appears with no new manifests written.

**A team is a directory:**

```bash
mkdir -p environments/billing
cat > environments/billing/environment.yaml <<'EOF'
apiVersion: platform.internal/v1alpha1
kind: Environment
metadata:
  name: billing-dev
spec:
  owner: {team: billing, contact: "#team-billing"}
  tier: dev
EOF

git add environments && git commit -m "Onboard billing" && git push
```

Within a few minutes: a new Application, an `Environment`, and a namespace with a quota and network policy.
A team onboarded itself with one directory and no platform involvement — which is the paved road, working.

```bash
kubectl -n argocd get applications
kubectl get env
kubectl get ns billing-dev
```

**And removal:**

```bash
git rm -r environments/billing && git commit -m "Offboard billing" && git push
```

Watch what happens, and check it matches what you decided in Step 2. If you're surprised, your sync policy
isn't what you thought.

## If it went wrong

| What you see | Usually means |
|---|---|
| No Applications generated | Generator matched nothing. For git directories, check the path glob and that the directories contain at least one file — empty dirs don't exist in Git. |
| `template: :1:1: executing "" at <.region>: map has no entry` | `goTemplate: true` missing, or a field name typo — which `missingkey=error` just caught for you. |
| Applications appear then immediately vanish | Generated names collide, so each render overwrites the last. Names must be unique per element. |
| Changes to the ApplicationSet don't reach existing Applications | Check the ApplicationSet controller is running: `kubectl -n argocd get deploy argocd-applicationset-controller`. |
| Team environment syncs but nothing appears | Argo CD applied the CR; your controller is what acts on it. Check the controller's logs — this is the two-altitudes thing from 06. |

## Stretch: a real second cluster

The honest version of multi-region needs a second cluster:

```bash
kind create cluster --name platform-dev-2
argocd cluster add kind-platform-dev-2 --name region-2
```

Then swap the list generator for a **cluster generator** filtered on a label, and regions become "whatever
clusters are registered", which is how you'd actually run it.

Expect friction, though less than you'd think — kind already puts every cluster on a shared `kind` docker
network. The problem is the *address*: kind's kubeconfig says `https://127.0.0.1:<host-port>`, which is right
for your laptop and meaningless inside a pod, so `argocd cluster add` configures the target successfully and
then fails validating it. Registering the cluster as a Secret with the internal address
(`https://<cluster>-control-plane:6443`) is the way through — see
[runbook 0003](../runbooks/0003-argocd-cluster-add-address-mismatch.md), which also covers the in-cluster
labelling trap that makes your local Application vanish.

## Reflection

1. Your list generator has three regions. One is unreachable during an outage. What do the other two do, and what does the ApplicationSet's status say?
2. A team edits `environments/payments/environment.yaml` to request 200 CPUs. What stops them, and at which layer? (Two of your three layers from ADR-0004 apply here; one doesn't.)
3. With a git directory generator, adding a directory grants a team an environment. Who can open that PR, and what does that make the review on that repo?

Next: 08 — progressive delivery with Argo Rollouts.
