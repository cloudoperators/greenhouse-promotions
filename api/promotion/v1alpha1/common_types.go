// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

// This file collects the simple, same-namespace "reference by name" types shared
// across the promotion CRDs. References that carry additional fields or are
// domain-specific (e.g. HelmChartReference, OciReference, SecretKeyReference)
// stay with their owning type.

// LocalSourceReference names a Source in the same namespace.
// Used by Revision, Environment, and Rollout to reference their owning Source.
type LocalSourceReference struct {
	// Name of the Source in the same namespace.
	Name string `json:"name"`
}

// SourceReference references a Source in the same namespace.
type SourceReference struct {
	// name of the referenced Source.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// EnvironmentReference references a single Environment in the same namespace.
type EnvironmentReference struct {
	// name of the referenced Environment.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// RolloutReference references a single Rollout in the same namespace.
type RolloutReference struct {
	// name of the referenced Rollout.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// RevisionReference names a Revision in the same namespace.
type RevisionReference struct {
	// name of the Revision.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// PluginReference names a Plugin in the same namespace.
type PluginReference struct {
	// name of the Plugin.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
