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

	kmeta "github.com/cloudoperators/greenhouse-promotions/api/meta"
)

// RevisionAnnotation is set on a Plugin and names the Revision it runs.
const RevisionAnnotation = "promotion.cloudoperators.dev/revision"

// RolloutReadyCondition is True once every target Plugin runs the Revision.
const RolloutReadyCondition = "Ready"

// RolloutPhase is where a Rollout stands.
// +kubebuilder:validation:Enum=Running;Succeeded;Failed
type RolloutPhase string

const (
	RolloutPhaseRunning   RolloutPhase = "Running"
	RolloutPhaseSucceeded RolloutPhase = "Succeeded"
	RolloutPhaseFailed    RolloutPhase = "Failed"
)

// RolloutSpec defines the desired state of Rollout
type RolloutSpec struct {
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

// RolloutTargets counts the target Plugins.
type RolloutTargets struct {
	// selected is the number of Plugins in spec.pluginRefs.
	Selected int32 `json:"selected"`

	// converged is the number of Plugins that run the Revision and are ready.
	Converged int32 `json:"converged"`
}

// RolloutStatus defines the observed state of Rollout.
type RolloutStatus struct {
	// phase is where the Rollout stands.
	// +optional
	Phase RolloutPhase `json:"phase,omitempty"`

	// startedAt is when the controller picked up the current spec.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// finishedAt is when the Rollout reached Succeeded or Failed.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// actor is who carried out the Rollout.
	// +optional
	Actor string `json:"actor,omitempty"`

	// targets counts the target Plugins.
	// +optional
	Targets *RolloutTargets `json:"targets,omitempty"`

	// conditions represent the current state of the Rollout resource.
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

// Rollout is the Schema for the rollouts API
type Rollout struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Rollout
	// +required
	Spec RolloutSpec `json:"spec"`

	// status defines the observed state of Rollout
	// +optional
	Status RolloutStatus `json:"status,omitzero"`
}

func (r *Rollout) GetConditions() []metav1.Condition {
	return r.Status.Conditions
}

func (r *Rollout) SetCondition(condition metav1.Condition) {
	r.Status.Conditions = kmeta.SetCondition(r.Status.Conditions, condition)
}

func (r *Rollout) GetReadyCondition() *metav1.Condition {
	return kmeta.GetConditionByType(r.Status.Conditions, kmeta.ReadyCondition)
}

// +kubebuilder:object:root=true

// RolloutList contains a list of Rollout
type RolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Rollout `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Rollout{}, &RolloutList{})
		return nil
	})
}
