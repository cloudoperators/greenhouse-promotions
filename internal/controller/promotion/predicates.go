// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package promotion

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ignoreDeletedResources returns a predicate that suppresses Update events for
// objects that are being deleted (deletionTimestamp set). This prevents the controller
// from re-queuing owned resources that are already on their way out.
func ignoreDeletedResources() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetDeletionTimestamp().IsZero()
		},
	}
}
