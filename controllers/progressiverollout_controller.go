/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1alpha1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	deploymentv1alpha1 "github.com/maruina/argocd-progressive-rollout-controller/api/v1alpha1"
)

const (
	ArgoCDSecretTypeLabel   = "argocd.argoproj.io/secret-type"
	ArgoCDSecretTypeCluster = "cluster"
)

// ProgressiveRolloutReconciler reconciles a ProgressiveRollout object
type ProgressiveRolloutReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type applicationWatchMapper struct {
	client.Client
	Log logr.Logger
}

// RolloutApp is a structure to use during a Rollout
type RolloutApp struct {
	ClusterName string
	Server      string
	App         argov1alpha1.Application
	Requeue     bool
}

// +kubebuilder:rbac:groups=deployment.skyscanner.net,resources=progressiverollouts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deployment.skyscanner.net,resources=progressiverollouts/status,verbs=get;update;patch

func (r *ProgressiveRolloutReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("progressiverollout", req.NamespacedName)
	pr := deploymentv1alpha1.ProgressiveRollout{}
	/* We trigger the reconciliation loop when:
	1. the ProgressiveRollout changed
	2. any of the Applications owned by spec.sourceRef changed
	*/

	// Get the ProgressiveRollout object
	if err := r.Get(ctx, req.NamespacedName, &pr); err != nil {
		log.Error(err, "unable to fetch ProgressiveRollout")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Iterate over the rollout stages
	for i, stage := range pr.Spec.Stages {

		log.V(1).Info("stage started", "stage", i)

		// ArgoCD stores the clusters as Kubernetes secrets
		//Get the spec.Clusters across all the secrets
		clusterSecretList := corev1.SecretList{}
		clusterSelector := metav1.AddLabelToSelector(&stage.Clusters.Selector, ArgoCDSecretTypeLabel, ArgoCDSecretTypeCluster)
		clusterSecretSelector, err := metav1.LabelSelectorAsSelector(clusterSelector)
		if err != nil {
			log.Error(err, "unable to create the clusters selector")
			return ctrl.Result{}, err
		}
		if err = r.List(ctx, &clusterSecretList, client.MatchingLabelsSelector{Selector: clusterSecretSelector}); err != nil {
			log.Error(err, "unable to list selected cluster")
		}
		log.V(1).Info("found selected clusters", "num_cluster", len(clusterSecretList.Items))

		// Get all spec.Requeue across all the secrets
		// This is not a very elegant solution, because ideally we would like to do the selection across the previous result.
		//It seems not possible because List is a client call to the k8s API and doesn't work on objects
		// TODO: find if there is a better way
		requeueSecretList := corev1.SecretList{}
		requeueSecretSelector, err := metav1.LabelSelectorAsSelector(&stage.Requeue.Selector)
		log.V(1).Info("requeue selector", "labels", requeueSecretSelector.String())
		if err != nil {
			log.Error(err, "unable to create the clusters selector")
			return ctrl.Result{}, err
		}
		if err = r.List(ctx, &requeueSecretList, client.MatchingLabelsSelector{Selector: requeueSecretSelector}); err != nil {
			log.Error(err, "unable to list selected cluster")
		}
		log.V(1).Info("found requeue clusters", "num_cluster", len(requeueSecretList.Items))

		// Get all the Application owned by the spec.sourceRef
		applicationList := argov1alpha1.ApplicationList{}
		var ownedApplications []argov1alpha1.Application
		err = r.List(ctx, &applicationList)
		if err != nil {
			log.Error(err, "unable to list applications")
			return ctrl.Result{}, err
		}
		for i, app := range applicationList.Items {
			for _, owner := range app.GetObjectMeta().GetOwnerReferences() {
				if pr.Spec.SourceRef.Name == owner.Name && *pr.Spec.SourceRef.APIGroup == owner.APIVersion && pr.Spec.SourceRef.Kind == owner.Kind {
					ownedApplications = append(ownedApplications, applicationList.Items[i])
					log.V(1).Info("found owned applications", "application", app.Name)
				}
			}
		}

		/* At this point we have:
		- clusterSecretList: a list of clusters we want to update in this stage
		- requeueSecretList: the clusters that we should requeue
		- the application that triggered the reconciliation loop
		What we need to do next:
		- find which cluster in clusterSecretList is also in requeueSecretList
		- find which cluster belong to which application, so we can pass it to the argoCD
		*/
		var rolloutApps []RolloutApp
		// Keep the applications matching the selected clusters
		for _, app := range ownedApplications {
			for _, cluster := range clusterSecretList.Items {
				name := string(cluster.Data["name"])
				server := string(cluster.Data["server"])
				if app.Spec.Destination.Server == server {

					log.V(1).Info("matched application and cluster", "application", app.Name, "cluster", name)

					// rolloutApps should be an ordered list so we have the requeued clusters at the end
					for _, rq := range requeueSecretList.Items {
						if rq.Name == cluster.Name {
							// This cluster matched on spec.clusters.selector AND spec.requeue.selector
							rolloutApps = append(rolloutApps, RolloutApp{ClusterName: cluster.Name, Server: server, App: app, Requeue: true})
						}
					}
				}
				rolloutApps = append([]RolloutApp{{ClusterName: cluster.Name, Server: server, App: app, Requeue: false}}, rolloutApps...)
			}
		}
		log.V(1).Info("syncTargets list built", "len", len(rolloutApps))

		/*
			Next steps:
			- select the maxClusters to update
			- select the maxUnavailable from the previous subgroup
			- handle requeue
			- use argocd cli to sync
			- wait until they are all sync
		*/

		// Select the maximum number of clusters to update in this stage
		maxRolloutApps := rolloutApps[0:stage.MaxClusters]
		// Select how many of them in parallel
		selectedRolloutApps, err := intstr.GetValueFromIntOrPercent(&stage.MaxUnavailable, stage.MaxClusters, true)
		if err != nil {
			log.Error(err, "failed to parse MaxUnavailable")
		}
		log.V(1).Info("selected rollout apps", "selectedRolloutApps", selectedRolloutApps)

		// Find all the Application OutOfSync
		var outOfSyncApps []RolloutApp
		for _, app := range maxRolloutApps {
			if app.App.Status.Sync.Status == argov1alpha1.SyncStatusCodeOutOfSync {
				outOfSyncApps = append(outOfSyncApps, app)
			}
		}

		for i := 0; i < selectedRolloutApps; i++ {
			rolloutApp := outOfSyncApps[i]
			if rolloutApp.Requeue {
				// Add logic here to handle requeue
			} else {
				// Check if the App is already synced
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *ProgressiveRolloutReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentv1alpha1.ProgressiveRollout{}).
		Watches(
			&source.Kind{Type: &argov1alpha1.Application{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: &applicationWatchMapper{r.Client, r.Log}},
		).
		Complete(r)
}

// Map maps an Application event to the matching ProgressiveRollout
func (a *applicationWatchMapper) Map(app handler.MapObject) []reconcile.Request {
	var requests []reconcile.Request
	pr, err := a.ListMatchingProgressiveRollout(a.Client, app.Meta)
	if err != nil {
		a.Log.Error(err, "error calling ListMatchingProgressiveRollout")
		return requests
	}
	if pr != nil {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      pr.Name,
			Namespace: pr.Namespace,
		}})
	}
	return requests
}

// ListMatchingProgressiveRollout filters the Application by looking at the OwnerReference
// and returns the ProgressiveRollout referencing it
func (a *applicationWatchMapper) ListMatchingProgressiveRollout(c client.Client, app metav1.Object) (*deploymentv1alpha1.ProgressiveRollout, error) {
	allProgressiveRollout := &deploymentv1alpha1.ProgressiveRolloutList{}
	err := c.List(context.Background(), allProgressiveRollout, &client.ListOptions{Namespace: app.GetNamespace()})

	if err != nil {
		return nil, err
	}

	// Check if the Application owner is reference by any ProgressiveRollout
	for _, pr := range allProgressiveRollout.Items {
		for _, owner := range app.GetOwnerReferences() {
			a.Log.V(1).Info("Matching Application with ProgressiveRollout", "Application", app.GetName(), "OwnerReference", owner.String(), "ProgressiveRolloutSourceRef", pr.Spec.SourceRef.String())
			if pr.Spec.SourceRef.Kind == owner.Kind && pr.Spec.SourceRef.Name == owner.Name && *pr.Spec.SourceRef.APIGroup == owner.APIVersion {
				a.Log.V(1).Info("Match found", "Application", app.GetName(), "ProgressiveRollout", pr.Name)
				return &pr, nil
			}
		}
	}

	// No match
	return nil, nil
}
