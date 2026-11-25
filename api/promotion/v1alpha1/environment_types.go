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

// HistoryResult is the outcome recorded for a rollout history entry.
// +kubebuilder:validation:Enum=Verified;Superseded;Failed
type HistoryResult string

const (
	// HistoryResultVerified means the rollout passed the gate (healthy through soak).
	// This is the automatic success outcome.
	HistoryResultVerified HistoryResult = "Verified"
	// HistoryResultSuperseded means a newer Revision was promoted before this one was
	// verified, so this entry was replaced rather than completed.
	HistoryResultSuperseded HistoryResult = "Superseded"
	// HistoryResultFailed means the rollout did not pass the gate.
	HistoryResultFailed HistoryResult = "Failed"
)

// Condition types reported on an Environment's status.conditions.
const (
	// ConditionReady is True once the target cohort has converged onto currentRevision.
	ConditionReady = "Ready"
	// ConditionHealthy reflects the current cohort health (True/False/Unknown).
	ConditionHealthy = "Healthy"
	// ConditionVerified is True once the gate passed for currentRevision; a downstream
	// Environment may then take verifiedRevision via verifiedIn.
	ConditionVerified = "Verified"
)

// EnvironmentSpec defines the desired state of Environment.
type EnvironmentSpec struct {
	// sourceRef points to the Source whose definition this Environment rolls out.
	// +required
	SourceRef SourceReference `json:"sourceRef"`

	// clusterSelector selects the Greenhouse Clusters that form this Environment's
	// slice of the fleet. A plain label selector; matching is by label, never by name.
	// +required
	ClusterSelector metav1.LabelSelector `json:"clusterSelector"`

	// source selects which Revision this Environment should run. Exactly one of the
	// nested fields must be set (one-of): requestFromSource (head of chain) or
	// verifiedIn (downstream, gated behind an upstream Environment).
	// +required
	Source EnvironmentSource `json:"source"`

	// gate is the health gate that decides when the current Revision is Verified.
	// +required
	Gate Gate `json:"gate"`

	// maxHistory is how many rollout history entries to retain (newest-first).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=10
	// +optional
	MaxHistory int32 `json:"maxHistory,omitempty"`
}

// EnvironmentSource is a one-of selecting the Revision to run.
// +kubebuilder:validation:MaxProperties=1
// +kubebuilder:validation:MinProperties=1
type EnvironmentSource struct {
	// requestFromSource tracks the Source directly: the Environment is a head of the
	// chain and takes versions the Source freezes according to its strategy.
	// +optional
	RequestFromSource *RequestFromSource `json:"requestFromSource,omitempty"`

	// verifiedIn gates this Environment behind an upstream Environment: it only runs
	// a Revision once that upstream has marked it Verified. A single reference.
	// +optional
	VerifiedIn *EnvironmentReference `json:"verifiedIn,omitempty"`
}

// RequestFromSource selects head-of-chain behavior: the Environment tracks the
// Source directly.
type RequestFromSource struct {
	// strategy decides which Revision from the Source's history the Environment
	// adopts, and how it advances:
	//   - Latest:     always track the newest ready Revision, skipping intermediate
	//                 versions.
	//   - Sequential: start at the oldest ready Revision still in the Source's history
	//                 and advance one version at a time through every version, each
	//                 gated by Verified. (Replays the retained history window, not
	//                 literally all versions ever produced.)
	// +kubebuilder:validation:Enum=Latest;Sequential
	// +kubebuilder:default=Latest
	// +optional
	Strategy RolloutStrategy `json:"strategy,omitempty"`
}

// RolloutStrategy selects how a requestFromSource Environment picks and advances
// Revisions from the Source's history.
type RolloutStrategy string

const (
	// StrategyLatest always tracks the newest ready Revision.
	StrategyLatest RolloutStrategy = "Latest"
	// StrategySequential starts at the oldest ready Revision in the history and
	// advances one version at a time after each is Verified.
	StrategySequential RolloutStrategy = "Sequential"
)

// Gate is the health gate that decides when the current Revision is Verified.
//
// Readiness is observed through the Rollout the Environment owns: the Rollout's
// Ready condition means its target Plugins have retargeted and converged. The gate
// passes once the Rollout is Ready and has stayed so for the soak duration. The
// Environment does not inspect Plugins itself; because it pins a single Revision at a
// time (~1:1), readiness is all-or-nothing.
type Gate struct {
	// soak is how long the cohort must stay continuously healthy after converging
	// before the Revision is marked Verified. A Go duration (e.g. "30m", "2h").
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`
	// +required
	Soak metav1.Duration `json:"soak"`

	// autoPromote, when false, holds before opening a Promotion until a human
	// approves (annotation); the gate still runs after the move is applied.
	// +kubebuilder:default=true
	// +optional
	AutoPromote bool `json:"autoPromote,omitempty"`
}

// EnvironmentStatus defines the observed state of Environment.
type EnvironmentStatus struct {
	// observedGeneration is the generation most recently reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// currentRevision references the Revision this Environment is currently on.
	// +optional
	CurrentRevision *RevisionReference `json:"currentRevision,omitempty"`

	// verifiedRevision references the Revision this Environment has verified through its
	// gate. It is set only once the Verified condition becomes True for that Revision,
	// and is the machine-readable signal a downstream Environment reads via verifiedIn.
	// +optional
	VerifiedRevision *RevisionReference `json:"verifiedRevision,omitempty"`

	// targets summarizes the Rollout this Environment owns and its convergence.
	// +optional
	Targets EnvironmentTargets `json:"targets,omitzero"`

	// history is the rollout history, newest-first, trimmed to spec.maxHistory.
	// +listType=atomic
	// +optional
	History []HistoryEntry `json:"history,omitempty"`

	// conditions represent the current state of the Environment resource.
	// Known types:
	// - "Ready": the target cohort has converged onto currentRevision
	// - "Healthy": the current cohort health (True/False/Unknown is the health signal)
	// - "Verified": the gate passed for currentRevision; verifiedRevision is set and a
	//   downstream Environment may take it via verifiedIn
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EnvironmentTargets summarizes the Promotions owned by this Environment and how many
// of them are ready (their Ready condition is True).
type EnvironmentTargets struct {
	// total is the number of Promotions this Environment owns.
	// +optional
	Total int32 `json:"total,omitempty"`

	// ready is how many of those Promotions report Ready=True.
	// +optional
	Ready int32 `json:"ready,omitempty"`
}

// HistoryEntry is one rollout record, keyed on the Revision the Environment moved
// through. There is a single long-lived Rollout per Environment (mutated in place),
// so this is the per-Revision audit trail rather than the Rollout object itself.
type HistoryEntry struct {
	// revision references the Revision for this rollout.
	// +optional
	Revision *RevisionReference `json:"revision,omitempty"`

	// enteredAt is when this Revision became the target.
	// +optional
	EnteredAt metav1.Time `json:"enteredAt,omitempty"`

	// verifiedAt is when the gate passed for this Revision, if it did.
	// +optional
	VerifiedAt *metav1.Time `json:"verifiedAt,omitempty"`

	// result is the outcome of this rollout.
	// +optional
	Result HistoryResult `json:"result,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Current",type=string,JSONPath=".status.currentRevision.name"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Verified",type=string,JSONPath=".status.conditions[?(@.type=='Verified')].status"

// Environment is the Schema for the environments API
type Environment struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Environment
	// +required
	Spec EnvironmentSpec `json:"spec"`

	// status defines the observed state of Environment
	// +optional
	Status EnvironmentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EnvironmentList contains a list of Environment
type EnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Environment `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Environment{}, &EnvironmentList{})
		return nil
	})
}

// GetConditions returns the Environment's status conditions.
// Implements lifecycle.RuntimeObject.
func (e *Environment) GetConditions() []metav1.Condition {
	return e.Status.Conditions
}

// SetCondition upserts a status condition on the Environment.
// Implements lifecycle.RuntimeObject.
func (e *Environment) SetCondition(c metav1.Condition) {
	e.Status.Conditions = kmeta.SetCondition(e.Status.Conditions, c)
}

// GetReadyCondition returns the Ready condition, or nil if unset.
// Implements lifecycle.RuntimeObject.
func (e *Environment) GetReadyCondition() *metav1.Condition {
	return kmeta.GetConditionByType(e.Status.Conditions, kmeta.ReadyCondition)
}
