# 06 — Argo CD and the app-of-apps

**Mode:** worked
**Time:** two hours
**You'll end up with:** your controller deployed and kept alive by Argo CD, from Git, with one root
Application that fans out to everything else.

Assumes Part 1 works and the repo is pushed to GitHub.

---

## The shift

Until now you've run the controller with `make run`, and `make deploy` would push it into the cluster from
your laptop. Both are fine for developing. Neither is how the platform should work.

From here, **the cluster is a projection of the repository**. If something is running, it's because a file
says so. If you want to change it, you change the file. `make deploy` becomes the wrong tool — not broken,
just no longer the write path — and that's the point of this example.

There are now two levels of reconciliation, and it's worth holding them apart:

- **Argo CD** reconciles the *platform* — is the controller deployed, at the right version, with the right config?
- **Your controller** reconciles *environments* — does each `Environment` have its namespace, quota and policy?

Same idea at two altitudes. Argo CD is to your controller what your controller is to a namespace.

## Step 1: Argo CD on kind

```bash
kubectl create namespace argocd
kubectl apply -n argocd --server-side --force-conflicts \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl -n argocd rollout status deploy/argocd-server --timeout=300s
```

`--server-side` is not optional. Argo CD's `applicationsets` CRD is larger than the 262144-byte limit on the
annotation that client-side apply uses to record state, so a plain `kubectl apply` fails with
`metadata.annotations: Too long`. Same failure mode as
[runbook 0001](../runbooks/0001-crd-annotation-size-limit.md) — you'll meet it again with any operator that
ships big CRDs.

Get the admin password and open the UI, which is genuinely useful for seeing sync state:

```bash
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo
kubectl -n argocd port-forward svc/argocd-server 8081:443    # https://localhost:8081, user: admin
```

## Step 2: get the image into the cluster

Argo CD will deploy your controller, but a kind cluster can't pull `controller:latest` from anywhere. Build
it and load it in:

```bash
cd environment-controller
make docker-build IMG=environment-controller:dev
kind load docker-image environment-controller:dev --name platform-dev
```

Then point the kustomize config at that tag, so what's in Git matches what's in the cluster:

```bash
cd config/manager && kustomize edit set image controller=environment-controller:dev && cd ../..
```

Check `config/manager/kustomization.yaml` — it should now name your image. Commit that; it's part of the
declared state.

One thing to fix while you're here: the image won't be pullable, so make sure the deployment doesn't try.
In `config/manager/manager.yaml`, on the container:

```yaml
        imagePullPolicy: IfNotPresent
```

Without it, `latest`-style tags default to `Always` and the pod sits in `ErrImagePull` forever on kind.

## Step 3: the layout

Create these in the repo root:

```
gitops/
  bootstrap/
    root-app.yaml
  platform/
    environment-controller.yaml
```

`gitops/platform/environment-controller.yaml` — the controller itself:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: environment-controller
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "0"
spec:
  project: default
  source:
    repoURL: https://github.com/sirdesmond/paved-road-platform.git
    targetRevision: main
    # Argo CD detects kustomization.yaml and renders it, so this is the same
    # thing `make deploy` would have applied — just from Git instead of a laptop.
    path: environment-controller/config/default
  destination:
    server: https://kubernetes.default.svc
    namespace: environment-controller-system
  syncPolicy:
    automated:
      prune: true      # delete from Git means delete from the cluster
      selfHeal: true   # hand edits get reverted
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true   # your CRD has a large schema; same reason as above
```

`gitops/bootstrap/root-app.yaml` — the one Application that manages the others:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: platform-root
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/sirdesmond/paved-road-platform.git
    targetRevision: main
    path: gitops/platform
    directory:
      recurse: true
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

That's the app-of-apps pattern, and it's smaller than its reputation suggests. One Application whose job is
to apply a directory of other Applications. Adding a platform component later means dropping a YAML file into
`gitops/platform/` — no cluster access needed, just a pull request.

## Step 4: bootstrap it

```bash
git add gitops environment-controller/config
git commit -m "Deploy the environment controller via Argo CD"
git push

kubectl apply -f gitops/bootstrap/root-app.yaml
```

That `kubectl apply` is the only imperative act, and you do it once per cluster. Everything after it comes
from Git. (Argo CD can even manage itself this way — worth doing eventually, and a good way to think about
where the bootstrap chain has to stop.)

---

## Checkpoint

```bash
kubectl -n argocd get applications
```

Both `platform-root` and `environment-controller` should reach `Synced / Healthy`. Then:

```bash
kubectl -n environment-controller-system get pods
kubectl get crd environments.platform.internal
```

Your controller is now running in-cluster, deployed by something other than you.

**Prove self-healing**, which is the whole reason for the automated sync policy:

```bash
kubectl -n environment-controller-system scale deploy/environment-controller-controller-manager --replicas=0
sleep 20
kubectl -n environment-controller-system get deploy
```

Back to 1. Argo CD noticed the cluster diverging from Git and corrected it. Exactly what your own controller
does for a hand-edited ResourceQuota, one level up.

**Prove Git is the write path:**

```bash
# change the replica count in config/manager/manager.yaml, then
git commit -am "Two replicas" && git push
kubectl -n argocd annotate app environment-controller argocd.argoproj.io/refresh=hard --overwrite
kubectl -n environment-controller-system get deploy -w
```

Default polling is every three minutes; the annotation just saves you waiting.

**Then confirm the whole thing still works end to end:**

```bash
kubectl apply -f environment-controller/config/samples/platform_v1alpha1_environment.yaml
kubectl get env
```

An `Environment` reconciled by a controller that you never deployed by hand.

## If it went wrong

| What you see | Usually means |
|---|---|
| `ErrImagePull` / `ImagePullBackOff` | Image not loaded into kind, or `imagePullPolicy` isn't `IfNotPresent`. Check with `docker exec -it platform-dev-control-plane crictl images \| grep environment`. |
| App stuck `OutOfSync`, no error | Open the UI. It shows the exact resource and diff blocking the sync, which the CLI makes you work for. |
| `metadata.annotations: Too long` | Missing `ServerSideApply=true` on an app with big CRDs. |
| `ComparisonError: repository not found` | Wrong `repoURL`, or the repo is private and Argo CD has no credentials. |
| Controller runs but can't create namespaces | RBAC. `make run` used *your* kubeconfig; in-cluster it uses its ServiceAccount. This is where a missing `+kubebuilder:rbac` marker finally bites — check the ConfigMap rule from example 03 made it into `config/rbac/role.yaml`. |

That last row is worth expecting rather than debugging. It's the single most common surprise when a controller
moves from `make run` to in-cluster.

## Reflection

1. `selfHeal: true` means a hand edit is reverted within minutes. When is that wrong — and how would someone legitimately make an emergency change?
2. You deleted `gitops/platform/environment-controller.yaml` from Git. With `prune: true`, what happens to the running `Environment` objects, and is that what you want?
3. Argo CD is deployed imperatively; everything else comes from Git. Where does that chain have to stop, and what does it mean for rebuilding this cluster from nothing?

Next: 07 — ApplicationSets, and making a second region a variable change.
