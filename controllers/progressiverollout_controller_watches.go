package controllers

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/maruina/argocd-progressive-rollout-controller/components"

	deploymentv1alpha1 "github.com/maruina/argocd-progressive-rollout-controller/api/v1alpha1"
)

// applicationWatchMapper is a support struct to filter Application events based on their owner
type applicationWatchMapper struct {
	client.Client
	Log logr.Logger
}

// secretWatchMapper is a support struct to filter Secret events
type secretWatchMapper struct {
	client.Client
	Log logr.Logger
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

// Map maps a Secret event to the matching ProgressiveRollout object
func (s *secretWatchMapper) Map(app handler.MapObject) []reconcile.Request {
	var requests []reconcile.Request
	pr, err := s.ListMatchingProgressiveRollout(s.Client, app.Meta)
	if err != nil {
		s.Log.Error(err, "error calling ListMatchingProgressiveRollout")
		return requests
	}
	if pr != nil {
		s.Log.V(1).Info("application event matched with a progressiverollout", "app", app.Meta.GetName(), "pr", pr.Name)
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      pr.Name,
			Namespace: pr.Namespace,
		}})
	}
	return requests
}

// ListMatchingProgressiveRollout filters the Secret by looking at which Progressive Rollout Cluster or Requeue selector match
func (s *secretWatchMapper) ListMatchingProgressiveRollout(c client.Client, secret metav1.Object) (*deploymentv1alpha1.ProgressiveRollout, error) {
	allProgressiveRollout := &deploymentv1alpha1.ProgressiveRolloutList{}

	err := c.List(context.Background(), allProgressiveRollout, &client.ListOptions{Namespace: secret.GetNamespace()})

	if err != nil {
		return nil, err
	}

	// Check if the ProgressiveRollout selector match with the event object
	for _, pr := range allProgressiveRollout.Items {
		for _, stage := range pr.Spec.Stages {
			clusterList, err := components.GetSecretListFromSelector(context.Background(), s.Client, &stage.Clusters.Selector)
			if err != nil {
				return nil, err
			}
			for _, c := range clusterList.Items {
				if c.Name == secret.GetName() {
					s.Log.V(1).Info("secret event matched with a progressiverollout", "secret", secret.GetName(),"selector", "cluster", "pr", pr.Name)
					return &pr, nil
				}
			}
			requeueList, err := components.GetSecretListFromSelector(context.Background(), s.Client, &stage.Requeue.Selector)
			if err != nil {
				return nil, err
			}
			for _, c := range requeueList.Items {
				if c.Name == secret.GetName() {
					s.Log.V(1).Info("secret event matched with a progressiverollout", "secret", secret.GetName(),"selector", "requeue", "pr", pr.Name)
					return &pr, nil
				}
			}
		}
	}

	// No match
	return nil, nil
}
