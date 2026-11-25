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

package promotion

import (
	"context"
	"fmt"
	"slices"

	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
)

const rolloutActor = "rollout-controller"

// RolloutReconciler reconciles a Rollout object
type RolloutReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=rollouts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=rollouts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=rollouts/finalizers,verbs=update
// +kubebuilder:rbac:groups=greenhouse.sap,resources=plugins,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *RolloutReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return lifecycle.Reconcile(ctx, r.Client, req.NamespacedName, &promotionv1alpha1.Rollout{}, r)
}

func (r *RolloutReconciler) GetEventRecorder() events.EventRecorder {
	return r.recorder
}

func (r *RolloutReconciler) EnsureDeleted(_ context.Context, _ lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	return ctrl.Result{}, lifecycle.Success, nil
}

func (r *RolloutReconciler) EnsureCreated(ctx context.Context, object lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	rollout := object.(*promotionv1alpha1.Rollout)
	if isFinished(rollout) {
		return ctrl.Result{}, reconcileResult(rollout), nil
	}

	now := metav1.Now()
	if rollout.Status.StartedAt == nil {
		rollout.Status.StartedAt = &now
	}
	rollout.Status.Actor = rolloutActor

	selected := len(rollout.Spec.PluginRefs)
	converged, missing, err := r.promote(ctx, rollout)
	if err != nil {
		return ctrl.Result{}, lifecycle.Failed, err
	}
	rollout.Status.Targets = &promotionv1alpha1.RolloutTargets{
		Selected:  int32(selected),
		Converged: int32(converged),
	}

	ready := metav1.Condition{Type: promotionv1alpha1.RolloutReadyCondition, ObservedGeneration: rollout.Generation}
	switch {
	case missing != "":
		rollout.Status.Phase = promotionv1alpha1.RolloutPhaseFailed
		ready.Status = metav1.ConditionFalse
		ready.Message = fmt.Sprintf("Plugin %s not found", missing)
	case converged == selected:
		rollout.Status.Phase = promotionv1alpha1.RolloutPhaseSucceeded
		ready.Status = metav1.ConditionTrue
		ready.Message = fmt.Sprintf("%d Plugins run %s", selected, rollout.Spec.RevisionRef.Name)
	default:
		rollout.Status.Phase = promotionv1alpha1.RolloutPhaseRunning
		ready.Status = metav1.ConditionFalse
		ready.Message = fmt.Sprintf("%d of %d Plugins run %s", converged, selected, rollout.Spec.RevisionRef.Name)
	}
	ready.Reason = string(rollout.Status.Phase)
	rollout.SetCondition(ready)
	if isFinished(rollout) {
		rollout.Status.FinishedAt = &now
	}

	return ctrl.Result{}, reconcileResult(rollout), nil
}

func reconcileResult(rollout *promotionv1alpha1.Rollout) lifecycle.ReconcileResult {
	switch rollout.Status.Phase {
	case promotionv1alpha1.RolloutPhaseSucceeded:
		return lifecycle.Success
	case promotionv1alpha1.RolloutPhaseFailed:
		return lifecycle.Failed
	default:
		return lifecycle.Pending
	}
}

func (r *RolloutReconciler) promote(ctx context.Context, rollout *promotionv1alpha1.Rollout) (converged int, missing string, err error) {
	revision := rollout.Spec.RevisionRef.Name
	plugins := make([]*greenhousev1alpha1.Plugin, 0, len(rollout.Spec.PluginRefs))
	for _, ref := range rollout.Spec.PluginRefs {
		plugin := &greenhousev1alpha1.Plugin{}
		err := r.Get(ctx, types.NamespacedName{Namespace: rollout.Namespace, Name: ref.Name}, plugin)
		if apierrors.IsNotFound(err) {
			return 0, ref.Name, nil
		}
		if err != nil {
			return 0, "", err
		}
		plugins = append(plugins, plugin)
	}
	for _, plugin := range plugins {
		if plugin.Annotations[promotionv1alpha1.RevisionAnnotation] != revision {
			base := plugin.DeepCopy()
			if plugin.Annotations == nil {
				plugin.Annotations = map[string]string{}
			}
			plugin.Annotations[promotionv1alpha1.RevisionAnnotation] = revision
			if err := r.Patch(ctx, plugin, client.MergeFrom(base)); err != nil {
				return 0, "", err
			}
			continue
		}
		if plugin.Status.IsReadyTrue() {
			converged++
		}
	}
	return converged, "", nil
}

func isFinished(rollout *promotionv1alpha1.Rollout) bool {
	return rollout.Status.Phase == promotionv1alpha1.RolloutPhaseSucceeded ||
		rollout.Status.Phase == promotionv1alpha1.RolloutPhaseFailed
}

func (r *RolloutReconciler) rolloutsForPlugin(ctx context.Context, plugin client.Object) []reconcile.Request {
	rollouts := &promotionv1alpha1.RolloutList{}
	if err := r.List(ctx, rollouts, client.InNamespace(plugin.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list Rollouts for Plugin", "plugin", plugin.GetName())
		return nil
	}
	var requests []reconcile.Request
	for i := range rollouts.Items {
		rollout := &rollouts.Items[i]
		if isFinished(rollout) {
			continue
		}
		references := slices.ContainsFunc(rollout.Spec.PluginRefs, func(ref promotionv1alpha1.PluginReference) bool {
			return ref.Name == plugin.GetName()
		})
		if references {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(rollout)})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *RolloutReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorder(rolloutActor)
	return ctrl.NewControllerManagedBy(mgr).
		For(&promotionv1alpha1.Rollout{}).
		Watches(&greenhousev1alpha1.Plugin{}, handler.EnqueueRequestsFromMapFunc(r.rolloutsForPlugin)).
		Named("promotion-rollout").
		Complete(r)
}
