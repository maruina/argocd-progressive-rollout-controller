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
	"errors"
	argov1alpha1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	"github.com/go-logr/logr"
	deploymentv1alpha1 "github.com/maruina/argocd-progressive-rollout-controller/api/v1alpha1"
	"github.com/maruina/argocd-progressive-rollout-controller/components"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strconv"
	"time"
)

const (
	ProgressiveRolloutRequeuedAtKey = "aprc.skyscanner.net/requeued-at"
	ProgressiveRolloutAttemptsKey   = "aprc.skyscanner.net/attempts"
)

// ProgressiveRolloutReconciler reconciles a ProgressiveRollout object
type ProgressiveRolloutReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
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
		r.Log.Info("stage started", "stage", stage.Name)

		// ArgoCD stores the clusters as Kubernetes secrets
		// clusterList is every cluster matching stage.Clusters.Selector, including the ones we want to requeue
		var clusterList corev1.SecretList
		var err error
		if stage.Clusters.TopologyKey != "" {
			clusterList, err = components.GetSecretListFromSelectorWithTopology(ctx, r.Client, &stage.Clusters.Selector, stage.Clusters.TopologyKey)
		} else {
			clusterList, err = components.GetSecretListFromSelector(ctx, r.Client, &stage.Clusters.Selector)
		}

		if err != nil {
			r.Log.Error(err, "failed to get clusters")
			return ctrl.Result{}, err
		}
		for _, cluster := range clusterList.Items {
			r.Log.V(1).Info("clusterList", "name", cluster.Name)
		}

		// requeueList is every cluster matching stage.Requeue.Selector
		requeueList, err := components.GetSecretListFromSelector(ctx, r.Client, &stage.Requeue.Selector)
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

			We want to remove clusters in the requeueList from the clusterList
			TODO: is there a better way?
		*/
		// stageList is every cluster we are updating in the stage
		var stageList corev1.SecretList
		if len(requeueList.Items) > 0 {
			for _, c := range clusterList.Items {
				for _, r := range requeueList.Items {
					if c.Name != r.Name {
						stageList.Items = append(stageList.Items, c)
					}
				}
			}
		} else {
			stageList.Items = clusterList.Items
		}
		components.SortClustersByName(&stageList)
		for _, cluster := range stageList.Items {
			r.Log.V(1).Info("stageList", "name", cluster.Name)
		}

		// ownedApplications has all the Applications owned by the spec.sourceRef
		ownedApplications, err := components.GetAppsFromOwner(ctx, r.Client, &pr.Spec.SourceRef)

		// Find Application targeting clusters in clusterList
		clusterApps := components.MatchSecretListWithApps(ownedApplications, &clusterList)
		// Find Applications targeting clusters in requeueList
		// We need to increment the requeue counter for those Applications
		requeueApps := components.MatchSecretListWithApps(ownedApplications, &requeueList)
		// Find Applications targeting clusters in stageList
		// We can safely update those Applications
		stageApps := components.MatchSecretListWithApps(ownedApplications, &stageList)
		for _, app := range stageApps {
			r.Log.V(1).Info("stageApps", "name", app.Name)
		}
		for _, app := range requeueApps {
			r.Log.V(1).Info("requeueApps", "name", app.Name)
		}

		// Get OutOfSync Applications so we can update them.
		toDoApps := components.GetAppsBySyncStatus(stageApps, argov1alpha1.SyncStatusCodeOutOfSync)
		for _, app := range toDoApps {
			r.Log.V(1).Info("toDoApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		// doneApps count against pr.stage.maxClusters
		doneApps := components.GetDoneApps(stageApps)
		for _, app := range doneApps {
			r.Log.V(1).Info("doneApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		// inProgressApps count against pr.stage.maxUnavailable
		inProgressApps := components.GetAppsByHealthStatus(stageApps, health.HealthStatusProgressing)
		for _, app := range inProgressApps {
			r.Log.V(1).Info("inProgressApps", "name", app.Name, "health", app.Status.Health.Status, "sync", app.Status.Sync.Status)
		}

		// maxClusters converts stage.MaxClusters
		maxClusters, err := intstr.GetValueFromIntOrPercent(&stage.MaxClusters, len(clusterApps), false)
		// stageMaxClusters is how many clusters to update before marking the stage as complete
		// doneApps count against the maxClusters quota
		stageMaxClusters := maxClusters - len(doneApps)
		// maxUnavailable converts stage.MaxUnavailable
		maxUnavailable, err := intstr.GetValueFromIntOrPercent(&stage.MaxUnavailable, stageMaxClusters, false)
		// stageMaxUnavailable is how many clusters to update at the same time
		stageMaxUnavailable := components.Min(maxUnavailable, len(stageApps)) - len(inProgressApps)

		r.Log.V(1).Info("rollout plan", "maxClusters", maxClusters, "maxUnavailable", maxUnavailable, "stageMaxClusters", stageMaxClusters, "stageMaxUnavailable", stageMaxUnavailable, "toDoApps", len(toDoApps), "inProgressApps", len(inProgressApps), "doneApps", len(doneApps), "stageApps", len(stageApps), "requeueApps", len(requeueApps))

		// If we want to update clusters and there are available
		if stageMaxClusters > 0 && len(toDoApps) > 0 {
			for i := 0; i < stageMaxUnavailable; i++ {
				name := toDoApps[i].Name
				r.Log.Info("syncing app", "app", name)
				if err = r.syncApp(name); err != nil {
					r.Log.Error(err, "failed to execute argocd command", "app", name)
				}
			}
		}

		// If we want to update more application than available, we would need a requeue cluster.
		if stageMaxClusters > len(toDoApps) && len(requeueApps) > 0 {
			for i := 0; i < maxClusters-len(stageApps); i++ {
				app := requeueApps[i]
				r.Log.Info("requeuing app", "app", app.Name)
				err = r.annotateApp(ctx, &stage.Requeue.Interval, stage.Requeue.Attempts, app)
				if err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: stage.Requeue.Interval.Duration}, nil
		}

		if len(doneApps) < stageMaxClusters || len(inProgressApps) > 0 {
			r.Log.Info("stage in progress", "stage", stage.Name)
			return ctrl.Result{}, nil
		} else {
			// TODO: remove all annotations
			r.Log.Info("stage complete", "stage", stage.Name)
		}

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
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: &secretWatchMapper{r.Client, r.Log}},
		).Complete(r)
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

func (r *ProgressiveRolloutReconciler) annotateApp(ctx context.Context, interval *metav1.Duration, maxAttempts int, app *argov1alpha1.Application) error {

	rawTime := time.Now()

	// Set defaults if there are no annotations
	if app.Annotations == nil {
		r.Log.V(1).Info("annotations missing", "app", app.Name)
		app.Annotations = map[string]string{
			ProgressiveRolloutRequeuedAtKey: rawTime.Format(time.RFC3339),
			ProgressiveRolloutAttemptsKey:   "1",
		}
	}

	attemptsRaw := app.Annotations[ProgressiveRolloutAttemptsKey]
	if len(attemptsRaw) == 0 {
		r.Log.V(1).Info("got annotations but attempts is missing", "app", app.Name)
		app.Annotations[ProgressiveRolloutAttemptsKey] = "1"
	}
	lastRequeuedAtRaw := app.Annotations[ProgressiveRolloutRequeuedAtKey]
	if len(lastRequeuedAtRaw) == 0 {
		r.Log.V(1).Info("got annotations but requeued-at is missing", "app", app.Name)
		app.Annotations[ProgressiveRolloutRequeuedAtKey] = rawTime.Format(time.RFC3339)
	}

	lastRequeuedAt, err := time.Parse(time.RFC3339, lastRequeuedAtRaw)
	if err != nil {
		app.Annotations[ProgressiveRolloutRequeuedAtKey] = rawTime.Format(time.RFC3339)
	}
	attempts, err := strconv.Atoi(attemptsRaw)
	if err != nil {
		app.Annotations[ProgressiveRolloutAttemptsKey] = "1"
	}

	r.Log.V(1).Info("found existing annotations", "app", app.Name, "annotations", app.Annotations)
	r.Log.V(1).Info("time computation", "lastRequeuedAt+interval", lastRequeuedAt.Add(interval.Duration).Format(time.RFC3339), "now", time.Now().Format(time.RFC3339))

	if lastRequeuedAt.Add(interval.Duration).Before(time.Now()) {
		r.Log.V(1).Info("cluster should be requeued", "app", app.Name)
		r.Log.V(1).Info("attempts computation", "app", app.Name, "attempts", attempts)
		if attempts > maxAttempts {
			r.Log.V(1).Info("max attempts reached", "app", app.Name, "attempts", attempts)
			err := errors.New("max attempts reached")
			return err
		}
		app.Annotations[ProgressiveRolloutAttemptsKey] = strconv.Itoa(attempts + 1)
		app.Annotations[ProgressiveRolloutRequeuedAtKey] = time.Now().Format(time.RFC3339)
		r.Log.V(1).Info("annotations updated", "app", app.Name, "annotations", app.Annotations)
	}
	err = r.Client.Update(ctx, app)
	return err
}
