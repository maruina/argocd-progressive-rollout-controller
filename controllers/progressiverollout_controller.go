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
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

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

	r.Log.Info("progressive rollout started", "pr", pr.Name)

	for _, stage := range pr.Spec.Stages {
		r.Log.V(1).Info("stage started", "stage", stage.Name)

		// ArgoCD stores the clusters as Kubernetes secrets
		allClusterList, err := utils.GetSecretListFromSelector(ctx, r.Client, &stage.Clusters.Selector)
		if err != nil {
			r.Log.Error(err, "failed to get clusters")
			return ctrl.Result{}, err
		}
		for _, cluster := range allClusterList.Items {
			r.Log.V(1).Info("allClusterList", "name", cluster.Name)
		}

		requeueList, err := utils.GetSecretListFromSelector(ctx, r.Client, &stage.Requeue.Selector)
		if err != nil {
			r.Log.Error(err, "failed to get requeue clusters")
			return ctrl.Result{}, err
		}
		for _, cluster := range requeueList.Items {
			r.Log.V(1).Info("requeueList", "name", cluster.Name)
		}

		/*
			Consider the following scenario:

			❯ kubectl get secrets -n argocd -l drained="true"
			NAME                                            TYPE     DATA   AGE
			cluster-eu-west-1a-1-control-plane-4073952145   Opaque   3      2d17h
			❯ kubectl get secrets -n argocd -l region=eu-west-1
			NAME                                            TYPE     DATA   AGE
			cluster-eu-west-1a-1-control-plane-4073952145   Opaque   3      2d17h
			cluster-eu-west-1b-1-control-plane-968703038    Opaque   3      2d17h

			We want to remove clusters in the requeueList from the allClusterList
			TODO: is there a better way?
		*/
		var stageList corev1.SecretList
		for _, c := range allClusterList.Items {
			for _, r := range requeueList.Items {
				if c.Name != r.Name {
					stageList.Items = append(stageList.Items, c)
				}
			}
		}
		utils.SortClustersByName(&stageList)
		for _, cluster := range stageList.Items {
			r.Log.V(1).Info("stageList", "name", cluster.Name)
		}

		// Get all the Application owned by the spec.sourceRef
		ownedApplications, err := utils.GetAppsFromOwner(ctx, r.Client, &pr.Spec.SourceRef)

		// Find Application targeting clusters in allClusterList
		allApps := utils.MatchSecretListWithApps(ownedApplications, &allClusterList)
		// Find Applications targeting clusters in stageList
		// We can safely update those Applications
		stageApps := utils.MatchSecretListWithApps(ownedApplications, &stageList)
		// Find Applications targeting clusters in requeueList
		// We need to increment the requeue counter for those Applications
		requeueApps := utils.MatchSecretListWithApps(ownedApplications, &requeueList)
		for _, app := range stageApps {
			r.Log.V(1).Info("stageApps", "name", app.Name)
		}
		for _, app := range requeueApps {
			r.Log.V(1).Info("requeueApps", "name", app.Name)
		}

		// Get OutOfSync Applications so we can update them.
		// We use allApps so we can count requeue clusters as well
		toDoApps := utils.GetAppsBySyncStatus(allApps, argov1alpha1.SyncStatusCodeOutOfSync)
		for _, app := range toDoApps {
			r.Log.V(1).Info("toDoApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		// Get Sync Applications as they count against pr.stage.maxClusters
		doneApps := utils.GetDoneApps(stageApps)
		for _, app := range doneApps {
			r.Log.V(1).Info("doneApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		inProgressApps := utils.GetAppsByHealthStatus(stageApps, health.HealthStatusProgressing)
		for _, app := range inProgressApps {
			r.Log.V(1).Info("inProgressApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		// maxClusters converts stage.MaxClusters
		maxClusters, err := intstr.GetValueFromIntOrPercent(&stage.MaxClusters, len(allApps), false)
		// stageMaxClusters is the maximum number of clusters to update before marking the stage as complete
		// doneApps count against the maxClusters quota
		stageMaxClusters := maxClusters - len(doneApps)
		// maxUnavailable converts stage.MaxUnavailable
		maxUnavailable, err := intstr.GetValueFromIntOrPercent(&stage.MaxUnavailable, stageMaxClusters, false)
		// stageMaxUnavailable is the maximum number of clusters to update in the stage
		stageMaxUnavailable := utils.Min(maxUnavailable, len(stageApps)) - len(inProgressApps)

		r.Log.V(1).Info("rollout plan", "maxClusters", maxClusters, "maxUnavailable", maxUnavailable, "stageMaxClusters", stageMaxClusters, "stageMaxUnavailable", stageMaxUnavailable, "toDoApps", len(toDoApps), "inProgressApps", len(inProgressApps), "doneApps", len(doneApps))

		if stageMaxClusters > 0 && len(stageApps) > 0 {
			for i := 0; i < stageMaxUnavailable; i++ {
				name := stageApps[i].Name
				r.Log.V(1).Info("syncing app", "app", name)
				if err = r.syncApp(name); err != nil {
					r.Log.Error(err, "failed to execute argocd command", "app", name)
				}
			}
		}

		// If we want to update more application than available, we would need a requeue cluster.
		if stageMaxClusters > 0 && len(requeueApps) > 0 && len(doneApps) == len(toDoApps) {
			for i := 0; i < maxClusters-len(stageApps); i++ {
				name := requeueApps[i].Name
				r.Log.V(1).Info("requeuing app", "app", name)
				// TODO: add annotation to keep track of requeue attempts
			}
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}

		r.Log.V(1).Info("stage complete", "stage", stage.Name)
	}
	r.Log.Info("rollout complete")
	return ctrl.Result{}, nil
}

func (r *ProgressiveRolloutReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentv1alpha1.ProgressiveRollout{}).
		Watches(
			&source.Kind{Type: &argov1alpha1.Application{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: &applicationWatchMapper{r.Client, r.Log}},
		).
		//TODO: Open another watch to the secrets
		Complete(r)
}

func (r *ProgressiveRolloutReconciler) syncApp(app string) error {
	cmd := exec.Command("argocd", "app", "sync", app, "--async", "--prune")
	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.String() == "exit status 20" {
				return nil
			}
		}
	}
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
