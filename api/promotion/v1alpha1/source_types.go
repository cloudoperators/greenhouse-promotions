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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	kmeta "github.com/cloudoperators/greenhouse-promotions/api/meta"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SourceSpec defines the desired state of Source
type SourceSpec struct {
	// Interval defines how often to reconcile the repository source
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +kubebuilder:default="1h"
	// +Optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// +kubebuilder:validation:XValidation:rule="has(self.oci) != has(self.git)",message="exactly one of oci or git must be set"
	Source *ArtifactSource `json:"source"`

	// Overrides are the PluginDefinition overrides to be applied
	// +Optional
	Override Override `json:"override,omitempty"`
}

// ArtifactSource indicates the artifact source type, either Git or OCI. Exactly one of Git or OCI must be set.
type ArtifactSource struct {
	// +Optional
	Git *GitSource `json:"git,omitempty"`
	// +Optional
	Oci *OCISource `json:"oci,omitempty"`
}

// GitSource indicates the Git source type, either a tag pattern or semverConstraint should be provided
type GitSource struct {
	URL  string `json:"url"`
	Path string `json:"path"`
	// SemverConstraint discovers tags satisfying a semver range (e.g. ">=1.14.0").
	// +optional
	SemverConstraint string `json:"semverConstraint,omitempty"`
	// TagPattern discovers tags matching a glob (e.g. perses/*).
	// +optional
	TagPattern string `json:"tagPattern,omitempty"`
	// Branch tracks the newest commit on a branch (e.g. main).
	// +optional
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
}

type OCISource struct {
	URL string `json:"url"`
	// SemverConstraint discovers tags satisfying a semver range (e.g. ">=1.14.0").
	// +optional
	SemverConstraint string         `json:"semverConstraint,omitempty"`
	SecretRef        *SecretRef     `json:"secretRef,omitempty"`
	LayerSelector    *LayerSelector `json:"layerSelector,omitempty"`
}

// LayerSelector identifies a specific layer within an OCI artifact.
type LayerSelector struct {
	MediaType string `json:"mediaType,omitempty"`
}

// SecretRef references a secret by name in the same namespace.
type SecretRef struct {
	Name string `json:"name"`
}

type Override struct {
	// Repository is the repository to override in the PluginDefinition .spec.helmChart.repository
	// +Optional
	Repository string `json:"repository,omitempty"`
	// OptionsOverride are the option values to override in the PluginDefinition .spec.options[]
	// +Optional
	OptionsOverride []OptionsOverride `json:"optionsOverride,omitempty"`
}

type OptionsOverride struct {
	// Name is the name of the option value to override in the PluginDefinition
	// +kubebuilder:validation:MinLength=1
	// +Required
	Name string `json:"name"`
	// Value is the value to set as Default in the PluginDefinition option
	// +Required
	Value *apiextensionsv1.JSON `json:"value"`
}

// SecretKeyReference specifies the secret and key containing the value.
type SecretKeyReference struct {
	// Name of the secret in the same namespace.
	Name string `json:"name"`
	// Key in the secret to select the value from.
	Key string `json:"key"`
}

// SourceStatus defines the observed state of Source.
type SourceStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Source is the Schema for the sources API
type Source struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Source
	// +required
	Spec SourceSpec `json:"spec"`

	// status defines the observed state of Source
	// +optional
	Status SourceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SourceList contains a list of Source
type SourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Source `json:"items"`
}

func (s *Source) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

func (s *Source) SetCondition(condition metav1.Condition) {
	s.Status.Conditions = kmeta.SetCondition(s.Status.Conditions, condition)
}

func (s *Source) GetReadyCondition() *metav1.Condition {
	return kmeta.GetConditionByType(s.Status.Conditions, kmeta.ReadyCondition)
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Source{}, &SourceList{})
		return nil
	})
}
