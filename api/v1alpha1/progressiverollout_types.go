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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProgressiveRolloutSpec defines the desired state of ProgressiveRollout
type ProgressiveRolloutSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//SourceRef references the resource, example an ApplicationSet, which owns ArgoCD Applications
	//+kubebuilder:validation:Required
	SourceRef corev1.TypedLocalObjectReference `json:"sourceRef"`
	// ProgressiveRolloutStage reference a list of ProgressiveRolloutStage
	//+kubebuilder:validation:Required
	Stages []*ProgressiveRolloutStage `json:"stages"`
}

// ProgressiveRolloutStage defines a rollout action
type ProgressiveRolloutStage struct {
	//Name is a human friendly name for the stage
	//+kubebuilder:validation:Required
	Name string `json:"name"`
	//MaxUnavailable is how many selected clusters to update in parallel
	MaxUnavailable intstr.IntOrString `json:"maxUnavailable,omitempty"`
	//MaxClusters is the maximum number of selected cluster to update
	MaxClusters intstr.IntOrString `json:"maxClusters,omitempty"`
	//Cluster is how to select the target clusters for the Rollout
	Clusters Cluster `json:"clusters"`
	//Requeue is when to postpone the cluster update
	Requeue Requeue `json:"requeue,omitempty"`
}

//Cluster defines how to select target clusters
type Cluster struct {
	//Selector is a label selector to get the clusters for the update
	Selector metav1.LabelSelector `json:"selector"`
	//TopologyKey is a string to group the clusters by a topology domain.
	TopologyKey string `json:"topologyKey,omitempty"`
}

//Requeue defines when to requeue a cluster before updating it
type Requeue struct {
	// Selector is a label selector to indicate when to requeue a cluster
	Selector metav1.LabelSelector `json:"selector"`
	// Interval is the time between attempts
	Interval metav1.Duration `json:"interval"`
	// Attempts is how many times try to update a cluster before failing the Rollout
	Attempts int `json:"attempts"`
}

// ProgressiveRolloutStatus defines the observed state of ProgressiveRollout
type ProgressiveRolloutStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true

// ProgressiveRollout is the Schema for the progressiverollouts API
type ProgressiveRollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProgressiveRolloutSpec   `json:"spec,omitempty"`
	Status ProgressiveRolloutStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProgressiveRolloutList contains a list of ProgressiveRollout
type ProgressiveRolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProgressiveRollout `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProgressiveRollout{}, &ProgressiveRolloutList{})
}
