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
	Git      *GitRepository   `json:"git"`
	Oci		 *OciRegistry	  `json:"oci"`

	// Overrides are the PluginDefinition overrides to be applied
	// +Optional
	Override Override `json:"override,omitempty"`
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

type OciRegistry struct {
	URL string `json:"url"`
	Ref *Reference `json:"ref"`
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
	LayerSelector *LayerSelector `json:"layerSelector,omitempty"`
}

// LayerSelector identifies a specific layer within an OCI artifact.
type LayerSelector struct {
	MediaType string `json:"mediaType,omitempty"`
}

type GitRepository struct {
	URL       string              `json:"url"`
	Path      string              `json:"path"`
	Ref       *Reference          `json:"ref"`
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
}

// SecretKeyReference specifies the secret and key containing the value.
type SecretKeyReference struct {
	// Name of the secret in the same namespace.
	Name string `json:"name"`
	// Key in the secret to select the value from.
	Key string `json:"key"`
}

// Reference contains the tag to resolve PluginDefinitions from
type Reference struct {
	// Tag is the semver tag to resolve PluginDefinitions
	Tag string `json:"tag"`
}

// SourceStatus defines the observed state of Source.
type SourceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Source resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
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

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Source{}, &SourceList{})
		return nil
	})
}
