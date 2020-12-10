package utils

import (
	"context"
	argov1alpha1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
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
	SortClustersByName(&clusterSecretList)
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
	SortAppsByName(ownedApplications)
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

func GetAppsBySyncStatus(apps []*argov1alpha1.Application, status argov1alpha1.SyncStatusCode) []*argov1alpha1.Application {
	var res []*argov1alpha1.Application
	for _, app := range apps {
		if app.Status.Sync.Status == status {
			res = append(res, app)
		}
	}
	return res
}

func GetCompleteApps(apps []*argov1alpha1.Application) []*argov1alpha1.Application {
	var res []*argov1alpha1.Application
	for _, app := range apps {
		if app.Status.Sync.Status == argov1alpha1.SyncStatusCodeSynced && app.Status.Health.Status != health.HealthStatusProgressing {
			res = append(res, app)
		}
	}
	return res
}

func GetAppsByHealthStatus(apps []*argov1alpha1.Application, h health.HealthStatusCode) []*argov1alpha1.Application {
	var res []*argov1alpha1.Application
	for _, app := range apps {
		if app.Status.Health.Status == h {
			res = append(res, app)
		}
	}
	return res
}

func GetAppsByNotHealthStatus(apps []*argov1alpha1.Application, h health.HealthStatusCode) []*argov1alpha1.Application {
	var res []*argov1alpha1.Application
	for _, app := range apps {
		if app.Status.Health.Status != h {
			res = append(res, app)
		}
	}
	return res
}

func SortAppsByName(apps []*argov1alpha1.Application) {
	sort.SliceStable(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
}

func SortClustersByName(clusters *corev1.SecretList) {
	sort.SliceStable(clusters.Items, func(i, j int) bool { return clusters.Items[i].Name < clusters.Items[j].Name })
}
