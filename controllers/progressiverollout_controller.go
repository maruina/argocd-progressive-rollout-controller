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
	"github.com/argoproj/gitops-engine/pkg/health"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os/exec"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/maruina/argocd-progressive-rollout-controller/utils"

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

// applicationWatchMapper is a support struct to filter Application events based on their owner
type applicationWatchMapper struct {
	client.Client
	Log logr.Logger
}

// RolloutItem is a support structure to use during a Rollout
type RolloutItem struct {
	App     *argov1alpha1.Application
	Requeue bool
}

// +kubebuilder:rbac:groups=deployment.skyscanner.net,resources=progressiverollouts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deployment.skyscanner.net,resources=progressiverollouts/status,verbs=get;update;patch

func (r *ProgressiveRolloutReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("progressiverollout", req.NamespacedName)

	// Get the ProgressiveRollout object
	pr := deploymentv1alpha1.ProgressiveRollout{}
	if err := r.Get(ctx, req.NamespacedName, &pr); err != nil {
		log.Error(err, "unable to fetch ProgressiveRollout", "object", pr.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Iterate over the rollout stages
	for i, stage := range pr.Spec.Stages {
		r.Log.WithValues("stage", i)

		// ArgoCD stores the clusters as Kubernetes secrets
		clusterSecretList, err := utils.GetSecretFromSelector(ctx, r.Client, &stage.Clusters.Selector)

		/*
			Ideally we would like to select the Requeue clusters from clusterSecretList.
			This is not possible because List is a client method and works on the k8s API and not an object.
			The solution is to get every secret with the spec.Requeue.selector and match them with clusterSecretList.
			If a secret is in both groups, it means it's a cluster we need to requeue
			TODO: is there a better way?
		*/
		requeueSecretList, err := r.GetSecretFromSelector(ctx, &stage.Requeue.Selector)

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

		/*
			Iterate over the Application list.
			If it's targeting one of the clusters, we want to update that application.
			If it's targeting on of the requeue clusters, append to the end of the list to reduce the likelihood to select it in the first pass.
		*/
		var selectedApps, requeueSelectedApps, rolloutApps []RolloutItem
		// Keep the applications matching the selected clusters
		for _, app := range ownedApplications {
			for _, cluster := range clusterSecretList.Items {
				name := string(cluster.Data["name"])
				server := string(cluster.Data["server"])
				log.V(1).Info("matching data", "secret name", name, "secret server", server, "app dest server", app.Spec.Destination.Server)
				if app.Spec.Destination.Server == server {

					log.V(1).Info("matched application and cluster", "application", app.Name, "cluster", name)

					for _, rq := range requeueSecretList.Items {
						if rq.Name == cluster.Name {
							log.V(1).Info("adding cluster to requeue list", "cluster", cluster.Name, "requeue cluster", rq.Name, "application", app.Name)
							// This cluster matched on spec.clusters.selector AND spec.requeue.selector
							requeueSelectedApps = append(requeueSelectedApps, RolloutItem{App: &app, Requeue: true})
						}
					}
					selectedApps = append(selectedApps, RolloutItem{App: &app, Requeue: false})
				}
			}
		}

		for _, app := range selectedApps {
			r.Log.V(1).Info("selectedApps", "app", app.App.Name)
		}
		for _, app := range requeueSelectedApps {
			r.Log.V(1).Info("requeueSelectedApps", "app", app.App.Name)
		}
		// Add the Requeue clusters at the end of the list
		selectedApps = append(selectedApps, requeueSelectedApps...)

		// Iterate over the selected Applications so we can build the rollout plan
		var syncCounter, progressingCounter int
		for _, item := range selectedApps {
			if item.App.Status.Sync.Status == argov1alpha1.SyncStatusCodeOutOfSync {
				// Application is out of sync -> we need to sync it
				rolloutApps = append(rolloutApps, item)
			} else if item.App.Status.Sync.Status == argov1alpha1.SyncStatusCodeSynced {
				syncCounter++
			} else if item.App.Status.Health.Status == health.HealthStatusProgressing {
				progressingCounter++
			}
		}

		log.V(1).Info("analyzed applications status", "sync", syncCounter, "progressing", progressingCounter, "outOfSync", len(rolloutApps))

		for _, app := range rolloutApps {
			log.V(1).Info("rolloutApps", "app", app.App.Name)
		}

		// Max number of clusters to sync
		rolloutClusters, err := intstr.GetValueFromIntOrPercent(&stage.MaxClusters, len(rolloutApps), true)
		// Remove the application that are already in sync.
		rolloutClusters -= syncCounter
		// Max number of clusters to sync per reconciliation
		rolloutUnavailable, err := intstr.GetValueFromIntOrPercent(&stage.MaxUnavailable, rolloutClusters, true)

		log.V(1).Info("planned rollout", "max clusters", rolloutClusters, "max unavailable", rolloutUnavailable)

		// Check if we have any app that is out of sync
		if len(rolloutApps) > 0 {
			// progressingClusters is part of the maxUnavailable quota
			for i := 0; i < (rolloutUnavailable - progressingCounter); i++ {
				if rolloutApps[i].Requeue {
					// TODO: annotation and retry after
				} else {
					log.V(1).Info("executing argocd app sync", "application", rolloutApps[i].App.Name)
					cmd := exec.Command("argocd", "app", "sync", rolloutApps[i].App.Name, "--async", "--prune")
					err = cmd.Run()
					if err != nil {
						log.Error(err, "error executing argocd cmd")
						return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
					}
					log.V(1).Info("executed argocd app sync", "app", rolloutApps[i].App.Name)
					// ArgoCD is syncing, requeue after 30s
					return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
				}
			}
			/*
				Protect against the case where we have OutOfSync apps but they are all Progressing
			*/
			log.V(1).Info("Protect against the case where we have OutOfSync apps but they are all Progressing")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}
		/*
			TODO: if we are here it means the Applications are all in sync. We need to check their health as we don't want to progress the Rollout.
			We still need to requeue the event as we want want to give the opportunity to fix the issue.
		*/
	}
	log.V(1).Info("complete rollout")
	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
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

func (r *ProgressiveRolloutReconciler) GetSecretFromSelector(ctx context.Context, selector *metav1.LabelSelector) (*corev1.SecretList, error) {
	// ArgoCD stores the clusters as Kubernetes secrets
	clusterSecretList := corev1.SecretList{}
	// Select based on the spec selector and the ArgoCD label
	clusterSelector := metav1.AddLabelToSelector(selector, ArgoCDSecretTypeLabel, ArgoCDSecretTypeCluster)
	clusterSecretSelector, err := metav1.LabelSelectorAsSelector(clusterSelector)
	if err != nil {
		r.Log.Error(err, "unable to create the clusters selector")
		return nil, err
	}
	if err = r.List(ctx, &clusterSecretList, client.MatchingLabelsSelector{Selector: clusterSecretSelector}); err != nil {
		r.Log.Error(err, "unable to list clusters")
	}
	r.Log.V(1).Info("found clusters", "total", len(clusterSecretList.Items))
	return &clusterSecretList, nil
}

// Map maps an Application event to the matching ProgressiveRollout object
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
			if pr.Spec.SourceRef.Kind == owner.Kind && pr.Spec.SourceRef.Name == owner.Name && *pr.Spec.SourceRef.APIGroup == owner.APIVersion {
				return &pr, nil
			}
		}
	}

	// No match
	return nil, nil
}
