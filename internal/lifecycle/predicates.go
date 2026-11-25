package lifecycle

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// IgnoreStatusUpdatePredicate drops update events that only change status, so
// a status write does not re-trigger reconcile.
func IgnoreStatusUpdatePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			if !cmp.Equal(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) || !cmp.Equal(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
				return true
			}
			if e.ObjectNew.GetGeneration() == e.ObjectOld.GetGeneration() && reflect.DeepEqual(e.ObjectNew.GetFinalizers(), e.ObjectOld.GetFinalizers()) {
				return e.ObjectNew.GetDeletionTimestamp() != nil
			}
			return true
		},
	}
}
