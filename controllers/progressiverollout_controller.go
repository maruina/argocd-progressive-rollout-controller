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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1alpha1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	deploymentv1alpha1 "github.com/maruina/argocd-progressive-rollout-controller/api/v1alpha1"
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

// +kubebuilder:rbac:groups=deployment.skyscanner.net,resources=progressiverollouts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deployment.skyscanner.net,resources=progressiverollouts/status,verbs=get;update;patch

func (r *ProgressiveRolloutReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("progressiverollout", req.NamespacedName)

	// your logic here

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
	pr, err := ListMatchingProgressiveRollout(a.Client, app.Meta)
	if err != nil {
		a.Log.Error(err, "error calling ListMatchingProgressiveRollout")
		return requests
	}
	requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
		Name: pr.Name,
		Namespace: pr.Namespace,
	}})
	return requests
}

// ListMatchingProgressiveRollout filters the Application by looking at the OwnerReference
// and returns the ProgressiveRollout referencing it
func ListMatchingProgressiveRollout(c client.Client, app metav1.Object ) (*deploymentv1alpha1.ProgressiveRollout, error) {
	allProgressiveRollout := &deploymentv1alpha1.ProgressiveRolloutList{}
	err := c.List(context.Background(), allProgressiveRollout,&client.ListOptions{Namespace: app.GetNamespace()})
	if err != nil {
		return nil, err
	}
	return nil, nil
}