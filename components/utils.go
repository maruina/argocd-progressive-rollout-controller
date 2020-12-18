package components

import (
	"context"
	argov1alpha1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
	"strconv"
)

const (
	ArgoCDSecretTypeLabel               = "argocd.argoproj.io/secret-type"
	ArgoCDSecretTypeCluster             = "cluster"
	ProgressiveRolloutRequeueTimeKey    = "aprc.skyscanner.net/requeued-at"
	ProgressiveRolloutRequeueAttemptKey = "aprc.skyscanner.net/attempt"
)

func GetSecretListFromSelector(ctx context.Context, c client.Client, selector *metav1.LabelSelector) (corev1.SecretList, error) {
	// ArgoCD stores the clusters as Kubernetes secrets
	clusterSecretList := corev1.SecretList{}
	// Select based on the spec selector and the ArgoCD label
	clusterSelector := metav1.AddLabelToSelector(selector, ArgoCDSecretTypeLabel, ArgoCDSecretTypeCluster)
	clusterSecretSelector, err := metav1.LabelSelectorAsSelector(clusterSelector)
	if err != nil {
		return clusterSecretList, err
	}
	if err = c.List(ctx, &clusterSecretList, client.MatchingLabelsSelector{Selector: clusterSecretSelector}); err != nil {
	}
	SortClustersByName(&clusterSecretList)
	return clusterSecretList, nil
}

func GetSecretListFromSelectorWithTopology(ctx context.Context, c client.Client, selector *metav1.LabelSelector, topology string) (corev1.SecretList, error) {
	clusterSecretList, err := GetSecretListFromSelector(ctx, c, selector)
	if err != nil {
		return clusterSecretList, err
	}
	clusterSecretList = SortClustersByTopologyKey(topology, &clusterSecretList)
	return clusterSecretList, nil
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
func GetDoneApps(apps []*argov1alpha1.Application) []*argov1alpha1.Application {
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

//SortAppsByName sort a list of applications in alphabetical order
func SortAppsByName(apps []*argov1alpha1.Application) {
	sort.SliceStable(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
}

//SortClustersByName sort a list of cluster in alphabetical order
func SortClustersByName(clusters *corev1.SecretList) {
	sort.SliceStable(clusters.Items, func(i, j int) bool { return clusters.Items[i].Name < clusters.Items[j].Name })
}

func SortClustersByTopologyKey(key string, clusters *corev1.SecretList) corev1.SecretList {
	var output corev1.SecretList
	// First sort input by name
	SortClustersByName(clusters)
	topologyMap := make(map[string][]corev1.Secret)
	for _, c := range clusters.Items {
		if val, ok := c.Labels[key]; ok {
			topologyMap[val] = append(topologyMap[val], c)
		}
	}

	// Get the most frequent topology key
	var maxLen int
	for _, v := range topologyMap {
		if len(v) >= maxLen {
			maxLen = len(v)
		}
	}

	for i := 0; i < maxLen; i++ {
		for _, v := range topologyMap {
			if len(v) > 0 {
				output.Items = append(output.Items, v[i])
				// Remove the element from the array
				v = append(v[:i], v[i+1:]...)
			}
		}
	}
	return output
}

//Min returns the minimum between two integers
func Min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

//IncrementRequeueAnnotation increment by one the requeue annotation key
func IncrementRequeueAnnotation(ctx context.Context, c client.Client, app *argov1alpha1.Application) error {
	val, err := GetRequeueAnnotation(ctx, c, app)
	if err != nil {
		return err
	}
	app.Annotations[ProgressiveRolloutRequeueAttemptKey] = strconv.Itoa(val + 1)
	err = c.Update(ctx, app)
	return err
}

//initRequeueAnnotation create the requeue annotations if missing
func initRequeueAnnotation(ctx context.Context, c client.Client, app *argov1alpha1.Application) error {
	if app.Annotations == nil {
		app.Annotations = map[string]string{
			ProgressiveRolloutRequeueAttemptKey: "0",
		}
	}
	if val, ok := app.Annotations[ProgressiveRolloutRequeueAttemptKey]; ok {
		_, err := strconv.Atoi(val)
		if err != nil {
			app.Annotations[ProgressiveRolloutRequeueAttemptKey] = "0"
		}
	} else {
		app.Annotations[ProgressiveRolloutRequeueAttemptKey] = "0"
	}
	err := c.Update(ctx, app)
	return err
}

//GetRequeueAnnotation returns the requeue annotation key value
func GetRequeueAnnotation(ctx context.Context, c client.Client, app *argov1alpha1.Application) (int, error) {
	err := initRequeueAnnotation(ctx, c, app)
	if err != nil {
		return 0, err
	}
	val, err := strconv.Atoi(app.Annotations[ProgressiveRolloutRequeueAttemptKey])
	return val, err
}

func ClustersHaveSameTopologyKey(clusterA, clusterB *corev1.Secret, topologyKey string) bool {
	if len(topologyKey) == 0 {
		return false
	}

	if clusterA.Labels == nil || clusterB.Labels == nil {
		return false
	}

	clusterALabel, okA := clusterA.Labels[topologyKey]
	clusterBLabel, okB := clusterB.Labels[topologyKey]

	// If found label in both nodes, check the label
	if okB && okA {
		return clusterALabel == clusterBLabel
	}

	return false
}

func HasRequeueAtAnnotation(app *argov1alpha1.Application) bool {
	return hasAnnotation(ProgressiveRolloutRequeueTimeKey, app)
}

func HasRequeueAttemptsAnnotation(app *argov1alpha1.Application) bool {
	return hasAnnotation(ProgressiveRolloutRequeueAttemptKey, app)
}

func hasAnnotations(app *argov1alpha1.Application) bool {
	if app.Annotations == nil {
		return false
	}
	return true
}

func hasAnnotation(key string, app *argov1alpha1.Application) bool {
	if !hasAnnotations(app) {
		return false
	}
	if _, ok := app.Annotations[key]; ok {
		return true
	}
	return false
}
