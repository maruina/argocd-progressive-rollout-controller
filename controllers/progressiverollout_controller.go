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

	for i, stage := range pr.Spec.Stages {
		r.Log.WithValues("stage", i)

		// ArgoCD stores the clusters as Kubernetes secrets
		clusterList, err := utils.GetSecretListFromSelector(ctx, r.Client, &stage.Clusters.Selector)
		if err != nil {
			r.Log.Error(err, "failed to get clusters")
			return ctrl.Result{}, err
		}
		for _, cluster := range clusterList.Items {
			r.Log.V(1).Info("clusterList", "name", cluster.Name)
		}

		requeueList, err := utils.GetSecretListFromSelector(ctx, r.Client, &stage.Requeue.Selector)
		if err != nil {
			r.Log.Error(err, "failed to get requeue clusters")
			return ctrl.Result{}, err
		}
		for _, cluster := range requeueList.Items {
			r.Log.V(1).Info("requeueList", "name", cluster.Name)
		}

		//TODO: Remove requeueList clusters from clusterList

		// Get all the Application owned by the spec.sourceRef
		ownedApplications, err := utils.GetAppsFromOwner(ctx, r.Client, &pr.Spec.SourceRef)

		// Find Applications targeting clusters in clusterList
		matchingApps := utils.MatchSecretListWithApps(ownedApplications, clusterList)
		// Find Applications targeting clusters in requeueList
		requeueMatchingApps := utils.MatchSecretListWithApps(ownedApplications, requeueList)
		for _, app := range matchingApps {
			r.Log.V(1).Info("selectedApps", "name", app.Name)
		}
		for _, app := range requeueMatchingApps {
			r.Log.V(1).Info("requeueSelectedApps", "name", app.Name)
		}

		// Get OutOfSync Applications so we can update them
		outOfSyncApps := utils.GetAppsBySyncStatus(matchingApps, argov1alpha1.SyncStatusCodeOutOfSync)
		for _, app := range outOfSyncApps {
			r.Log.V(1).Info("outOfSyncApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		// Get Sync Applications as they count against pr.stage.maxClusters
		syncApps := utils.GetAppsBySyncStatus(matchingApps, argov1alpha1.SyncStatusCodeSynced)
		for _, app := range syncApps {
			r.Log.V(1).Info("syncApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		progressingApps := utils.GetAppsByHealthStatus(matchingApps, health.HealthStatusProgressing)
		for _, app := range progressingApps {
			r.Log.V(1).Info("notHealthyApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		//TODO: Remove progressingApps from outOfSyncApps

		// maxClusters converts stage.MaxClusters
		maxClusters, err := intstr.GetValueFromIntOrPercent(&stage.MaxClusters, len(outOfSyncApps), false)
		// stageMaxClusters is the maximum number of clusters to update before marking the stage as complete
		// A Sync or Progressing cluster counts against the MaxClusters quota
		stageMaxClusters := maxClusters - len(syncApps)
		// stageMaxUnavailable is the maximum number of clusters to update
		stageMaxUnavailable, err := intstr.GetValueFromIntOrPercent(&stage.MaxUnavailable, stageMaxClusters, false)
		r.Log.V(1).Info("rollout plan", "maxClusters", maxClusters, "stageMaxClusters", stageMaxClusters, "stageMaxUnavailable", stageMaxUnavailable)

		if len(outOfSyncApps) > 0 {
			for i := 0; i < stageMaxUnavailable; i++ {
				name := outOfSyncApps[i].Name
				r.Log.V(1).Info("syncing app", "app", name)
				if err = r.syncApp(name); err != nil {
					r.Log.Error(err, "failed to execute argocd command", "app", name)
				}
			}
		}
	}
	r.Log.Info("reconciliation complete")
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

func (r *ProgressiveRolloutReconciler) syncApp(app string) error {
	cmd := exec.Command("argocd", "app", "sync", app, "--async", "--prune")
	err := cmd.Run()
	return err
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
		a.Log.V(1).Info("application event matched with a progressiverollout", "app", app.Meta.GetName(), "pr", pr.Name)
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
