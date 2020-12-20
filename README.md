# argocd-progressive-rollout-controller
Progressive Rollout controller for ArgoCD ApplicationSet.

**Status: Proof of Concept (actively looking for feedback)**

## Why
[ApplicationSet](https://github.com/argoproj-labs/applicationset) is being developed as the solution to replace the `app-of-apps` pattern.

While ApplicationSet is great to programmatically generate Applications, you will still need to solve _how_ to update the Applications. 

If you enable the [auto-sync](https://argoproj.github.io/argo-cd/user-guide/auto_sync/) policy, that will update _all_ your generated Application at the same time.

This might not be a problem if you have only one production cluster, but organizations with tens or hundreds of production clusters need to avoid a global rollout and to release new versions in a safer way.

The `argocd-progressive-rollout-operator` solves this problem by allowing operators to decide **how** they want to update their Applications.

## Concepts

- Watch for Applications and Secrets events, using a source reference to track ownership.
- Use label selectors to retrieve the cluster list.
- Relies on properly labelling ArgoCD secrets.
- A cluster can be requeued. This mean the operator will try to update it at the end of the stage. This is useful if you want to temporary bring a cluster offline for maintenance without having to freeze the deployments.
- Topology key to allow grouping clusters. For example, you might want to update few clusters, but only one per region.
- _(TODO)_ Bake time to allow a deployment to soak for a certain amount of time before moving to the next stage.
- _(TODO)_ Webhooks to call specific endpoints during the stage. This can be useful to trigger load or smoke tests.
- _(TODO)_ Metric checks.

## Watch it in action

Click on the image to watch the video.

[![ArgoCD Progressive Rollout Controller Demo](http://img.youtube.com/vi/xoaemCbiqzo/0.jpg)](http://www.youtube.com/watch?v=xoaemCbiqzo "ArgoCD Progressive Rollout Controller Demo")

## Example Spec

In the following example we are going to update 2 clusters in EMEA, before updating one region at the time.

If a cluster - the secret object - has the label `drained="true"`, it will be requeued.

```yaml
apiVersion: deployment.skyscanner.net/v1alpha1
kind: ProgressiveRollout
metadata:
  name: progressiverollout-sample
  namespace: argocd
spec:
  # the object owning the target applications
  sourceRef:
    apiGroup: argoproj.io/v1alpha1
    kind: ApplicationSet
    name: my-app-set
  # the rollout steps
  stages:
    - name: canary in EMEA
      # how many clusters to update in parallel
      maxUnavailable: 2
      # how many cluster to update from the clusters selector result
      maxClusters: 2
      # which clusters to update
      clusters:
        selector:
          matchLabels:
            area: emea
      # how to group the the clusters selector result
      topologyKey: region
      # which clusters to requeue
      requeue:
        selector:
          matchLabels:
            drained: "true"
        # how many times to reueue a cluster before failing the rollout
        attempts: 5
        # how often to try to update a reueued cluster
        interval: 30m
    - name: eu-west-1
      maxUnavailable: 25%
      maxClusters: 100%
      clusters:
        selector:
          matchLabels:
            region: eu-west-1
      requeue:
        selector:
          matchLabels:
            drained: "true"
        attempts: 5
        interval: 30m
    - name: eu-central-1
      maxUnavailable: 25%
      maxClusters: 100%
      clusters:
        selector:
          matchLabels:
            region: eu-central-1
      requeue:
        selector:
          matchLabels:
            drained: "true"
        attempts: 5
        interval: 30m
```

## Development

In order to start developing the progressive rollout controller, you need to have a local installation of [kubebuilder](https://book.kubebuilder.io/quick-start.html#installation).

## Testing locally with kind

- Install kind: <https://kind.sigs.k8s.io/docs/user/quick-start/#installation>
- Create the kind clusters. You need at least one control cluster and one target cluster.

```console
kind create cluster --name eu-west-1a-1
kind create cluster --name eu-west-1a-2
kind create cluster --name eu-central-1a-1
kind create cluster # this is the control cluster for argocd
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

- Install the ArgoCD CLI: <https://argoproj.github.io/argo-cd/getting_started/#2-download-argo-cd-cli>

- Register the target clusters in Argo CD. Please note that you need to use the internal address that will not work from your CLI. The solution is to exec into the `argocd-server` pod and to run the commands from there

```console
kubectl exec -it -n argocd argocd-server-6987c9748c-6x27q -- argocd login argocd-server.argocd.svc.cluster.local:443

# From another shell
kind get kubeconfig --name eu-west-1a-1 --internal | pbcopy

# Back into the argocd-server pod
cat >eu-west-1a-1 <<EOF
<PASTE>
EOF

argocd cluster add kind-eu-west-1a-1 --kubeconfig eu-west-1a-1
```

- Create the `infrabin` namespace in every target cluster

```console
kubectl create ns infrabin --context kind-eu-west-1a-1

kubectl create ns infrabin --context kind-eu-central-1a-1

kubectl create ns infrabin --context kind-eu-west-1b-1
```

- Create a sample `applicationset`. You can use [config/samples/appset-goinfra.yaml](./config/samples/appset-goinfra.yaml).

- Install the CRDs into the control cluster

```console
make install
```

- Create a sample `progressiverollout`.  You can use [config/samples/deployment_v1alpha1_progressiverollout.yaml](./config/samples/deployment_v1alpha1_progressiverollout.yaml).

- Port-forward to the `argocd-server` service and login.

```console
kubectl port-forward -n argocd svc/argocd-server 8080:443

# From another shell
argocd login localhost:8080
```

- Run the operator

```console
make run
```

## TODO

- [x] Add topologyKey for grouping clusters with the same selector
- [ ] Add Progressdeadline to allow detecting a stuck deployment
- [ ] Add annotation on Requeue clusters and handle failure
- [ ] Failure handling
- [ ] Add ProgressiveRollout Status
- [ ] Finalizer
- [ ] More than one tests :(
- [ ] Break the scheduling logic into a separate component for better testing
- [ ] Validation: one ApplicationSet can be referenced only by one ProgressiveRollout object
- [ ] Validation: sane defaults
- [ ] Support Argo CD Projects
