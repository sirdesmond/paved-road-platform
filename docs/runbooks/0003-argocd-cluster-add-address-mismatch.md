# Runbook 0003 — `argocd cluster add` fails validating a cluster it just configured

**Symptom:** registering a second cluster appears to work, then dies at the last step:

```
ServiceAccount "argocd-manager" already exists in namespace "kube-system"
ClusterRole "argocd-manager-role" updated
ClusterRoleBinding "argocd-manager-role-binding" updated
Using existing bearer token secret "argocd-manager-long-lived-token"
fatal: rpc error: code = Unknown desc = error getting server version:
  Get "https://127.0.0.1:60837/version?timeout=32s": dial tcp 127.0.0.1:60837: connect: connection refused
```

No cluster is registered. `kubectl -n argocd get secrets -l argocd.argoproj.io/secret-type=cluster` is empty.

## What's confusing about it

Every line before the failure succeeded. The CLI clearly reached the target cluster — it created a
ServiceAccount, a ClusterRole and a binding there. So "connection refused" reads like a flake.

## What's actually happening

Two different processes connect to the target cluster, from two different places:

1. **The `argocd` CLI**, running on your machine, uses your kubeconfig to install the ServiceAccount. Your kubeconfig says `https://127.0.0.1:60837`, which is a port kind published on your host. Works.
2. **The Argo CD server**, running as a pod, then validates the cluster by fetching its version — using the *same address it was handed*. Inside that pod, `127.0.0.1` is the pod itself. Nothing is listening. Refused.

The address is correct for the CLI and meaningless for the server. `argocd cluster add` has no flag to
override the URL it stores, so this can't be fixed by arguing with the command.

This affects any kind/k3d/minikube setup where the kubeconfig uses a host-local address. It does not affect
real clusters, where the API server address is routable from everywhere.

## Fix: register the cluster declaratively

A cluster registration in Argo CD is just a Secret. Build it with an address that means the same thing inside
the cluster — for kind, the control-plane container's name on the shared `kind` docker network:

```bash
kind get kubeconfig --name <target> --internal | grep server:
# https://<target>-control-plane:6443
```

The API server certificate covers that name and the container IP, so TLS verification still works.

The ServiceAccount and token already exist from the failed run, so reuse them:

```bash
TOKEN=$(kubectl --context kind-<target> -n kube-system \
  get secret argocd-manager-long-lived-token -o jsonpath='{.data.token}' | base64 -d)

CA=$(kubectl --context kind-<target> -n kube-system \
  get secret argocd-manager-long-lived-token -o jsonpath='{.data.ca\.crt}')

kubectl --context kind-<argocd-cluster> -n argocd create secret generic cluster-region-2 \
  --from-literal=name=region-2 \
  --from-literal=server=https://<target>-control-plane:6443 \
  --from-literal=config="{\"bearerToken\":\"$TOKEN\",\"tlsClientConfig\":{\"insecure\":false,\"caData\":\"$CA\"}}"

kubectl --context kind-<argocd-cluster> -n argocd label secret cluster-region-2 \
  argocd.argoproj.io/secret-type=cluster \
  region=<region> paved-road/platform=true
```

`CA` is deliberately not decoded — `caData` expects base64, and it already is.

Verify:

```bash
argocd cluster list          # should show the cluster; Unknown until an Application targets it
```

## Gotchas around this

**`argocd.argoproj.io/secret-type=cluster` is load-bearing.** Without it the secret is inert and
`argocd cluster list` stays empty, with no error anywhere.

**`Unknown / not being monitored` is not a failure.** Argo CD only connects to a cluster once something
targets it. It flips to a version and `Successful` when the first Application lands.

**The in-cluster cluster has no Secret.** It's implicit, so it has no labels, so any `matchLabels` selector in
a cluster generator excludes it — and the Application for your local cluster silently disappears. Create a
secret for it too if you're selecting on labels:

```bash
kubectl -n argocd create secret generic cluster-in-cluster \
  --from-literal=name=in-cluster \
  --from-literal=server=https://kubernetes.default.svc \
  --from-literal=config='{"tlsClientConfig":{"insecure":false}}'
```

No credentials needed — Argo CD uses its own ServiceAccount for the cluster it runs in.

**`curl` isn't in the argocd-server image.** It's distroless, so the obvious connectivity test fails with
`executable file not found`. Use a throwaway pod:

```bash
kubectl -n argocd run netcheck --rm -it --restart=Never --image=curlimages/curl -- \
  curl -sk https://<target>-control-plane:6443/version
```

## Security note

That cluster secret holds a cluster-admin bearer token. It belongs in the cluster and must never reach Git —
a concrete instance of the wider point that secrets aren't in Git, and therefore a rebuild-from-Git does not
restore them. Whatever provisions secrets (External Secrets, a vault) is a prerequisite for claiming the
platform is reproducible.

## The general lesson

**An address is only meaningful relative to who resolves it.** `127.0.0.1` in a kubeconfig means your laptop
to a CLI and the pod itself to a controller. The same string, two answers.

Worth recognising the shape: whenever a tool hands configuration to a *different* process to act on, ask
whether the values still mean the same thing over there. Applies to kubeconfig server URLs, `localhost` in
container env vars, file paths passed into containers, and hostnames in CI.
