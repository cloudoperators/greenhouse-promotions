/*
Copyright 2026.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// RevisionAnnotation is set on a Plugin and names the Revision it runs.
const RevisionAnnotation = "promotion.cloudoperators.dev/revision"

// PromotionReadyCondition is True once every target Plugin runs the Revision.
const PromotionReadyCondition = "Ready"

// PromotionPhase is where a Promotion stands.
// +kubebuilder:validation:Enum=Running;Succeeded;Failed
type PromotionPhase string

const (
	PromotionPhaseRunning   PromotionPhase = "Running"
	PromotionPhaseSucceeded PromotionPhase = "Succeeded"
	PromotionPhaseFailed    PromotionPhase = "Failed"
)

// RevisionReference names a Revision in the namespace of the Promotion.
type RevisionReference struct {
	// name of the Revision.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// PluginReference names a Plugin in the namespace of the Promotion.
type PluginReference struct {
	// name of the Plugin.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// PromotionSpec defines the desired state of Promotion
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type PromotionSpec struct {
	// revisionRef is the Revision the target Plugins move to.
	// +required
	RevisionRef RevisionReference `json:"revisionRef"`

	// pluginRefs are the Plugins that move to the Revision.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	PluginRefs []PluginReference `json:"pluginRefs"`
}

// PromotionTargets counts the target Plugins.
type PromotionTargets struct {
	// selected is the number of Plugins in spec.pluginRefs.
	Selected int32 `json:"selected"`

	// converged is the number of Plugins that run the Revision and are ready.
	Converged int32 `json:"converged"`
}

// PromotionStatus defines the observed state of Promotion.
type PromotionStatus struct {
	// phase is where the Promotion stands. Succeeded and Failed are final.
	// +optional
	Phase PromotionPhase `json:"phase,omitempty"`

	// startedAt is when the controller picked up the Promotion.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// finishedAt is when the Promotion reached a final phase.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// actor is who carried out the Promotion.
	// +optional
	Actor string `json:"actor,omitempty"`

	// targets counts the target Plugins.
	// +optional
	Targets *PromotionTargets `json:"targets,omitempty"`

	// conditions represent the current state of the Promotion resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.spec.revisionRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Selected",type=integer,JSONPath=`.status.targets.selected`
// +kubebuilder:printcolumn:name="Converged",type=integer,JSONPath=`.status.targets.converged`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Promotion is the Schema for the promotions API
type Promotion struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Promotion
	// +required
	Spec PromotionSpec `json:"spec"`

	// status defines the observed state of Promotion
	// +optional
	Status PromotionStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PromotionList contains a list of Promotion
type PromotionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Promotion `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Promotion{}, &PromotionList{})
		return nil
	})
}
