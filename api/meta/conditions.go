package meta

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionType is a valid condition of a resource.
type ConditionType string

// ConditionReason is a valid reason for a condition of a resource.
type ConditionReason string

const (
	// ReadyCondition indicates the resource is ready.
	ReadyCondition ConditionType = "Ready"
	// DeleteCondition tracks the delete phase of a resource.
	DeleteCondition ConditionType = "Delete"
)

// SetCondition upserts a condition by type. LastTransitionTime is preserved
// when the status is unchanged and stamped with metav1.Now otherwise.
func SetCondition(conditions []metav1.Condition, incoming metav1.Condition) []metav1.Condition {
	for i, existing := range conditions {
		if existing.Type != incoming.Type {
			continue
		}
		if existing.Status == incoming.Status {
			incoming.LastTransitionTime = existing.LastTransitionTime
		} else if incoming.LastTransitionTime.IsZero() {
			incoming.LastTransitionTime = metav1.Now()
		}
		conditions[i] = incoming
		return conditions
	}
	if incoming.LastTransitionTime.IsZero() {
		incoming.LastTransitionTime = metav1.Now()
	}
	return append(conditions, incoming)
}

// RemoveCondition removes the condition of the given type.
func RemoveCondition(conditions []metav1.Condition, t ConditionType) []metav1.Condition {
	return slices.DeleteFunc(conditions, func(c metav1.Condition) bool {
		return c.Type == string(t)
	})
}

// GetConditionByType returns a pointer to the condition of the given type, or nil.
func GetConditionByType(conditions []metav1.Condition, t ConditionType) *metav1.Condition {
	if i := slices.IndexFunc(conditions, func(c metav1.Condition) bool {
		return c.Type == string(t)
	}); i >= 0 {
		return &conditions[i]
	}
	return nil
}
