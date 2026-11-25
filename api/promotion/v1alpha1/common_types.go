// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

// LocalSourceReference names a Source in the same namespace.
// Used by Revision, Environment, and Rollout to reference their owning Source.
type LocalSourceReference struct {
	// Name of the Source in the same namespace.
	Name string `json:"name"`
}
