package utils

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ArgoCDSecretTypeLabel   = "argocd.argoproj.io/secret-type"
	ArgoCDSecretTypeCluster = "cluster"
)

func GetSecretFromSelector(ctx context.Context, c client.Client, selector *metav1.LabelSelector) (*corev1.SecretList, error) {
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
