# argocd-progressive-rollout-controller
Progressive Rollout controller for ArgoCD ApplicationSet

## Development

In order to start developing the progressive rollout controller, you need to:

- Install kind: <https://kind.sigs.k8s.io/docs/user/quick-start/#installation>
- Install kubebuilder: <https://book.kubebuilder.io/quick-start.html#installation>
- Create a kind cluster named `control`

```console
kind create cluster --name eu-west-1a-1
kind create cluster --name eu-west-1a-2
kind create cluster --name eu-central-1b-1
kind create cluster
```

- Install ArgoCD

```console
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
# Print admin password
kubectl get pods -n argocd -l app.kubernetes.io/name=argocd-server -o name | cut -d'/' -f 2
```

- Install ArgoCD ApplicationSet controller: <https://github.com/argoproj-labs/applicationset>. There is already an image built at `maruina/argocd-applicationset:v0.1.0`.

```console
cd <path/to/applicationset>
IMAGE="maruina/argocd-applicationset:v0.1.0" make deploy
```
