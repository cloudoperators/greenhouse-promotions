// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	promotionsmeta "github.com/cloudoperators/greenhouse-promotions/api/meta"
)

const (
	// EnvironmentFinalizerPrefix is the prefix for per-Environment finalizers on a Revision.
	// Format: environment.promotion.cloudoperators.dev/<environment-name>
	EnvironmentFinalizerPrefix = "environment.promotion.cloudoperators.dev/"

	// RevisionLabelSource is the label key recording which Source owns a Revision.
	RevisionLabelSource = "promotion.cloudoperators.dev/source"
)

// RevisionOptionType is the type of a configurable option carried from the PluginDefinition.
// +kubebuilder:validation:Enum=string;secret;bool;int;list;map
type RevisionOptionType string

const (
	RevisionOptionTypeString RevisionOptionType = "string"
	RevisionOptionTypeSecret RevisionOptionType = "secret"
	RevisionOptionTypeBool   RevisionOptionType = "bool"
	RevisionOptionTypeInt    RevisionOptionType = "int"
	RevisionOptionTypeList   RevisionOptionType = "list"
	RevisionOptionTypeMap    RevisionOptionType = "map"
)

// ReplicationStatusType is the outcome of chart replication to a mirror registry.
// +kubebuilder:validation:Enum=Replicated;Failed;Skipped
type ReplicationStatusType string

const (
	ReplicationStatusReplicated ReplicationStatusType = "Replicated"
	ReplicationStatusFailed     ReplicationStatusType = "Failed"
	ReplicationStatusSkipped    ReplicationStatusType = "Skipped"
)

// RevisionOption is a configurable option carried from the PluginDefinition.
// Defaults here are pristine; overrides from spec.overrides are applied separately at consume time.
type RevisionOption struct {
	// Name of the option.
	Name string `json:"name"`
	// Default is the pristine default value from the upstream PluginDefinition.
	// +optional
	Default *apiextensionsv1.JSON `json:"default,omitempty"`
	// Description is a human-readable explanation of the option.
	// +optional
	Description string `json:"description,omitempty"`
	// Required indicates this option must be provided by a consumer.
	Required bool `json:"required"`
	// Type is the value type.
	Type RevisionOptionType `json:"type"`
}

// HelmChartReference identifies a Helm chart in a repository.
type HelmChartReference struct {
	// Name of the Helm chart.
	Name string `json:"name"`
	// Repository is the chart repository URL (https:// or oci://).
	Repository string `json:"repository"`
	// Version is the SemVer chart version.
	Version string `json:"version"`
}

// RevisionDefinitionSpec is the PluginDefinition spec frozen at a specific upstream ref.
// Keep in sync with greenhouse's PluginDefinitionSpec for fields the promotions system uses.
type RevisionDefinitionSpec struct {
	// Version of the PluginDefinition.
	Version string `json:"version"`
	// DisplayName is a human-readable label.
	// +optional
	DisplayName string `json:"displayName,omitempty"`
	// Description provides additional details.
	// +optional
	Description string `json:"description,omitempty"`
	// Icon is a URL or juno icon name.
	// +optional
	Icon string `json:"icon,omitempty"`
	// HelmChart specifies where the Helm chart can be found.
	// +optional
	HelmChart *HelmChartReference `json:"helmChart,omitempty"`
	// Options are the configurable options for this PluginDefinition.
	// +optional
	Options []RevisionOption `json:"options,omitempty"`
}

// RevisionDefinition holds the pristine PluginDefinition body resolved from upstream.
// Overrides are never merged into this field; they live in RevisionSpec.Overrides.
type RevisionDefinition struct {
	// Kind is "PluginDefinition" or "ClusterPluginDefinition".
	Kind string `json:"kind"`
	// Spec is the full PluginDefinition spec at the resolved ref.
	Spec RevisionDefinitionSpec `json:"spec"`
}

// OptionOverride replaces the default value of a named option at consume time.
type OptionOverride struct {
	// Name matches a RevisionOption name in the definition.
	Name string `json:"name"`
	// Value replaces the option's default value.
	Value *apiextensionsv1.JSON `json:"value"`
}

// RevisionSpec defines the desired state of Revision.
// Written once at creation by the Source controller; immutable thereafter.
// The owning Source is recorded via ownerReference; use a field indexer on ownerUID to look up Revisions by Source.
type RevisionSpec struct {
	// Ref is the upstream ref (git tag, OCI tag) this Revision was resolved from.
	Ref string `json:"ref"`
	// Digest is the sha256 of the definition file at spec.ref.
	Digest string `json:"digest"`
	// Definition holds the pristine, unmodified PluginDefinition resolved at spec.ref.
	Definition RevisionDefinition `json:"definition"`
	// OptionOverrides are copied from the owning Source at creation time and applied at
	// consume time by the promotions-server. Never merged into spec.definition.
	// +optional
	OptionOverrides []OptionOverride `json:"optionOverrides,omitempty"`
}

// RevisionHelmChartStatus records the Flux chart sync and mirroring outcome.
type RevisionHelmChartStatus struct {
	// SourceRef is the name of the owned Flux HelmChart resource.
	SourceRef string `json:"sourceRef"`
	// MirroredRepository is the internal OCI registry URL the chart was mirrored to.
	// +optional
	MirroredRepository string `json:"mirroredRepository,omitempty"`
	// MirroredVersion is the chart version in the mirrored repository.
	// +optional
	MirroredVersion string `json:"mirroredVersion,omitempty"`
}

// RevisionStatus defines the observed state of Revision.
type RevisionStatus struct {
	// ObservedGeneration is the metadata.generation this status was computed from.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// ResolvedAt is when this Revision was last successfully reconciled.
	// +optional
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`
	// Version mirrors spec.definition.spec.version for quick inspection.
	// +optional
	Version string `json:"version,omitempty"`
	// HelmChart reflects the Flux chart sync and OCI mirroring outcome.
	// +optional
	HelmChart *RevisionHelmChartStatus `json:"helmChart,omitempty"`
	// Conditions reflect the Revision's reconcile state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=rev
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.version`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Referenced",type=string,JSONPath=`.status.conditions[?(@.type=="Referenced")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Revision is the Schema for the revisions API.
type Revision struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec RevisionSpec `json:"spec"`
	// +optional
	Status RevisionStatus `json:"status,omitzero"`
}

// FluxHelmChartResourceName returns the name used for the owned Flux HelmChart resource.
// The Revision name already encodes source and version (e.g. cert-manager-1-20-1).
func (r *Revision) FluxHelmChartResourceName() string {
	return r.Name
}

// GetConditions returns the Revision's status conditions.
func (r *Revision) GetConditions() []metav1.Condition {
	return r.Status.Conditions
}

// SetCondition upserts a condition on the Revision.
func (r *Revision) SetCondition(c metav1.Condition) {
	r.Status.Conditions = promotionsmeta.SetCondition(r.Status.Conditions, c)
}

// GetReadyCondition returns the Ready condition, or nil.
func (r *Revision) GetReadyCondition() *metav1.Condition {
	return promotionsmeta.GetConditionByType(r.Status.Conditions, promotionsmeta.ReadyCondition)
}

// +kubebuilder:object:root=true

// RevisionList contains a list of Revision.
type RevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Revision `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Revision{}, &RevisionList{})
		return nil
	})
}
