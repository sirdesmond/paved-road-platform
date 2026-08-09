# Worked examples

A build-along track for this platform. The point isn't to end up with my code in your repo, it's for you to
be able to write it again without me.

## How the format works

Each example runs in three modes, and the track gets progressively less helpful on purpose:

- **Worked.** Every file, every command, every marker explained. You type it in, run it, and it works. Read the explanations rather than just pasting, since the next one assumes you understood this one.
- **Faded.** Structure and the hard parts are given, with clearly marked gaps. You fill them in. This is where the learning actually happens, and it will feel slower than the worked ones. That's the point.
- **Solo.** A goal, a checklist to verify against, and hints only if you're stuck. No code.

Every example ends with a **checkpoint**: exact commands and the output you should see. If the checkpoint
doesn't pass, don't move on. Debugging a small thing now beats debugging a large thing in three examples' time.

## Ground rules

- **Run everything locally on kind.** No AWS spend while learning. The cloud parts come later and only once the pattern is clear.
- **Commit after every checkpoint.** You want to be able to walk back to the last working state.
- **When something breaks, write down what it was** before you fix it. That's your runbook material, and it's the most interesting content you'll produce.
- **Don't skip the "why" sections.** In an interview, nobody asks you to recite `kubebuilder` commands. They ask why the resource is cluster-scoped, or how you handle a partial failure mid-reconcile.

## The track

### Part 1 — The controller (start here)

| # | Example | Mode | You'll end up with |
|---|---|---|---|
| **01** | [Scaffold and the Environment API](./01-scaffold-and-api.md) | worked | A cluster-scoped `Environment` CRD installed on kind, with validation and printer columns |
| **02** | [The reconciler](./02-reconciler.md) | faded | An `Environment` that creates a namespace, quota and network policy, and reports status |
| **03** | [Finalizers and clean teardown](./03-finalizers.md) | faded | Cleanup for the things garbage collection can't see — and a feel for when a finalizer is the wrong tool |
| **04** | [Tier defaults and validation](./04-tier-defaults.md) | solo | Defaults that vary by tier, and bad specs rejected with messages that help |
| **05** | [Tests with envtest](./05-tests.md) | worked | Controller tests that run in CI — tier defaults, the CEL rules, and a reconcile that fails halfway |

### Part 2 — Delivery

| # | Example | Mode |
|---|---|---|
| **06** | [Argo CD and the app-of-apps](./06-argocd-app-of-apps.md) | worked |
| **07** | [ApplicationSets, and a region as a variable change](./07-applicationsets.md) | faded |
| 08 | Argo Rollouts canary with automated analysis | faded |
| 09 | Admission policy: VAP and MAP in audit, then enforce | faded |

### Part 3 — The API and the cloud

| # | Example | Mode |
|---|---|---|
| 10 | `platform-api`: validate a request, render, open a PR | worked |
| 11 | `platformctl`: the CLI over the same contract | solo |
| 12 | Terraform: VPC, EKS and IRSA, with teardown | worked |
| 13 | Multi-account and the second region | faded |
| 14 | SLOs, alerts and a runbook that gets used | faded |

## One thing to sort out first: no spaces in the path

Make splits on whitespace, so a directory like `~/Workspace/platform engineering/` breaks every
kubebuilder-generated Makefile. The symptom is confusing rather than obvious:

```
make: Circular /Users/you/Workspace/platform <- /Users/you/Workspace/platform dependency dropped.
bash: /Users/you/Workspace/platform: No such file or directory
```

Make has taken the path up to the space as a target in its own right. There's no escaping trick that reliably
fixes it. Keep the whole tree somewhere without spaces:

```bash
mv ~/Workspace/"platform engineering" ~/Workspace/platform-engineering
```

Worth doing before you start rather than halfway through, and it'll save you the same problem with Terraform
wrappers and shell scripts later.

## Prerequisites for Part 1

```bash
brew install go kind kubectl kubebuilder
```

The brew formula doesn't include the envtest binaries (kube-apiserver, etcd) that the release tarball ships.
Nothing before example 05 needs them, and the Makefile kubebuilder generates will fetch them via
`setup-envtest` the first time you run `make test`. So: fine to ignore, just don't be alarmed when your first
test run pauses to download things.

Check you're on Go 1.22 or newer, since the controller-runtime version we use needs it:

```bash
go version
kubebuilder version
kind version
```

You'll want a cluster to work against:

```bash
kind create cluster --name platform-dev
kubectl cluster-info --context kind-platform-dev
```

Start with [example 01](./01-scaffold-and-api.md).
