package utils

import (
	"context"
	argov1alpha1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ArgoCDSecretTypeLabel   = "argocd.argoproj.io/secret-type"
	ArgoCDSecretTypeCluster = "cluster"
)

func GetSecretListFromSelector(ctx context.Context, c client.Client, selector *metav1.LabelSelector) (*corev1.SecretList, error) {
	// ArgoCD stores the clusters as Kubernetes secrets
	clusterSecretList := corev1.SecretList{}
	// Select based on the spec selector and the ArgoCD label
	clusterSelector := metav1.AddLabelToSelector(selector, ArgoCDSecretTypeLabel, ArgoCDSecretTypeCluster)
	clusterSecretSelector, err := metav1.LabelSelectorAsSelector(clusterSelector)
	if err != nil {
		return nil, err
	}
	if err = c.List(ctx, &clusterSecretList, client.MatchingLabelsSelector{Selector: clusterSecretSelector}); err != nil {
	}
	return &clusterSecretList, nil
}

func GetAppsFromOwner(ctx context.Context, c client.Client, owner *corev1.TypedLocalObjectReference) ([]*argov1alpha1.Application, error) {

	applicationList := argov1alpha1.ApplicationList{}
	var ownedApplications []*argov1alpha1.Application
	err := c.List(ctx, &applicationList)
	if err != nil {
		return nil, err
	}
	for i, app := range applicationList.Items {
		for _, appOwner := range app.GetObjectMeta().GetOwnerReferences() {
			if owner.Name == appOwner.Name && *owner.APIGroup == appOwner.APIVersion && owner.Kind == appOwner.Kind {
				ownedApplications = append(ownedApplications, &applicationList.Items[i])
			}
		}
	}
	return ownedApplications, nil
}

func MatchSecretListWithApps(apps []*argov1alpha1.Application, list *corev1.SecretList) []*argov1alpha1.Application {
	var match []*argov1alpha1.Application
	for _, app := range apps {
		for _, cluster := range list.Items {
			server := string(cluster.Data["server"])
			if app.Spec.Destination.Server == server {
				match = append(match, app)
			}
		}
	}
	return match
}
