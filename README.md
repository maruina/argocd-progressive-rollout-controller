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
kind create cluster --name eu-central-1a-1
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
cd $GOPATH/src/github.com/argoproj-labs/applicationset
IMAGE="maruina/argocd-applicationset:v0.1.0" make deploy
```

❯ kubectl exec -it -n argocd argocd-server-6987c9748c-6x27q -- argocd login argocd-server.argocd.svc.cluster.local:443
❯ kind get kubeconfig --name eu-west-1a-1 --internal | pbcopy

cat >eu-west-1a-1 <<EOF
<PASTE>
EOF

argocd@argocd-server-6987c9748c-p4qxs:~$ argocd cluster add kind-eu-central-1a-1 --kubeconfig eu-central-1a-1

❯ kubectl label secrets -n argocd cluster-eu-central-1a-1-control-plane-3371478133 cluster=eu-central-1a-1 region=eu-central-1 --overwrite
secret/cluster-eu-central-1a-1-control-plane-3371478133 labeled
❯ kubectl label secrets -n argocd cluster-eu-west-1a-1-control-plane-4073952145 cluster=eu-west-1a-1 region=eu-west-1 --overwrite
secret/cluster-eu-west-1a-1-control-plane-4073952145 labeled
❯ kubectl label secrets -n argocd cluster-eu-west-1b-1-control-plane-968703038 cluster=eu-west-1b-1 region=eu-west-1 --overwrite
secret/cluster-eu-west-1b-1-control-plane-968703038 labeled

❯ kubectl create ns infrabin --context kind-eu-west-1a-1
namespace/infrabin created
❯ kubectl create ns infrabin --context kind-eu-central-1a-1
namespace/infrabin created
❯ kubectl create ns infrabin --context kind-eu-west-1b-1
namespace/infrabin created

## TODO

- [ ] Add topologyKey
- [ ] Add Progressdeadline
- [ ] Add annotation on Requeue clusters and handle failure
- [ ] Failure handling
- [ ] Add ProgressiveRollout Status
- [ ] Finalizer
- [ ] Tests :(
- [ ] Break the scheduling logic into a separate component for better testing
- [ ] Validation: one ApplicationSet can be referenced only by one ProgressiveRollout object
- [ ] Validation: sane defaults