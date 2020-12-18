package components

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const TestNamespace = "default"

var _ = Describe("Utils", func() {

	ctx := context.Background()

	BeforeEach(func() {
		clusterEuWest1a1 := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-eu-west-1a-1", Namespace: TestNamespace, Labels: map[string]string{
			"cluster":             "eu-west-1a-1",
			"region":              "eu-west-1",
			"area":                "emea",
			ArgoCDSecretTypeLabel: ArgoCDSecretTypeCluster,
		}}}
		clusterEuWest1b1 := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-eu-west-1b-1", Namespace: TestNamespace, Labels: map[string]string{
			"cluster":             "eu-west-1b-1",
			"region":              "eu-west-1",
			"area":                "emea",
			ArgoCDSecretTypeLabel: ArgoCDSecretTypeCluster,
		}}}
		clusterEuCentral1a1 := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-eu-central-1a-1", Namespace: TestNamespace, Labels: map[string]string{
			"cluster":             "eu-central-1a-1",
			"region":              "eu-central",
			"area":                "emea",
			ArgoCDSecretTypeLabel: ArgoCDSecretTypeCluster,
		}}}
		clusterEuCentral1b1 := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-eu-central-1b-1", Namespace: TestNamespace, Labels: map[string]string{
			"cluster":             "eu-central-1b-1",
			"region":              "eu-central",
			"area":                "emea",
			ArgoCDSecretTypeLabel: ArgoCDSecretTypeCluster,
		}}}
		clusterApSoutheast1a1 := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-ap-southeast-1a-1", Namespace: TestNamespace, Labels: map[string]string{
			"cluster":             "ap-southeast-1a-1",
			"region":              "ap-southeast",
			"area":                "apac",
			ArgoCDSecretTypeLabel: ArgoCDSecretTypeCluster,
		}}}
		Expect(k8sClient.Create(ctx, clusterEuWest1a1)).To(Succeed())
		Expect(k8sClient.Create(ctx, clusterEuWest1b1)).To(Succeed())
		Expect(k8sClient.Create(ctx, clusterEuCentral1a1)).To(Succeed())
		Expect(k8sClient.Create(ctx, clusterEuCentral1b1)).To(Succeed())
		Expect(k8sClient.Create(ctx, clusterApSoutheast1a1)).To(Succeed())
	})

	Describe("Cluster selectors", func() {
		It("Should group clusters by topology domain when a topology key is present", func() {
			selector := &metav1.LabelSelector{MatchLabels: map[string]string{
				"area": "emea",
			}}
			topologyKey := "region"
			actual, err := GetSecretListFromSelectorWithTopology(context.Background(), k8sClient, selector, topologyKey)
			Expect(err).To(BeNil())
			Expect(actual.Items).ShouldNot(BeEmpty())

			var actualNames []string
			for _, c := range actual.Items {
				actualNames = append(actualNames, c.Name)
			}
			expectedNames := []string{"cluster-eu-central-1a-1", "cluster-eu-west-1a-1", "cluster-eu-central-1b-1", "cluster-eu-west-1b-1"}

			Expect(actualNames).To(Equal(expectedNames))

		})
	})
})
