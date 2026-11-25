// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package promotion

import (
	"context"
	"fmt"
	"strings"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	kmeta "github.com/cloudoperators/greenhouse-promotions/api/meta"
	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
	revisionphases "github.com/cloudoperators/greenhouse-promotions/internal/phases/revision"
)

const (
	conditionHelmChartReady = "HelmChartReady"
	conditionReferenced     = "Referenced"
)

// RevisionReconciler reconciles a Revision object.
type RevisionReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                events.EventRecorder
	MaxConcurrentReconciles int
	OCIMirroringEnabled     bool
}

// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=revisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=revisions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=revisions/finalizers,verbs=update
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=sources,verbs=get
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories/status,verbs=get
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts/status,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *RevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.Recorder = mgr.GetEventRecorder("revision-controller")

	return ctrl.NewControllerManagedBy(mgr).
		Named("promotion-revision").
		For(&promotionv1alpha1.Revision{}).
		Owns(&sourcev1.HelmRepository{}, builder.WithPredicates(ignoreDeletedResources())).
		Owns(&sourcev1.HelmChart{}, builder.WithPredicates(ignoreDeletedResources())).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}

// GetEventRecorder implements lifecycle.Reconciler.
func (r *RevisionReconciler) GetEventRecorder() events.EventRecorder {
	return r.Recorder
}

// Reconcile is the main reconciliation entry point; delegates to lifecycle.Reconcile.
func (r *RevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return lifecycle.Reconcile(ctx, r.Client, req.NamespacedName, &promotionv1alpha1.Revision{}, r)
}

// EnsureCreated implements lifecycle.Reconciler and runs all creation/update phases.
func (r *RevisionReconciler) EnsureCreated(ctx context.Context, obj lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	revision := obj.(*promotionv1alpha1.Revision)

	r.initializeConditions(revision)

	if revision.Spec.Definition.Spec.HelmChart == nil {
		return ctrl.Result{}, lifecycle.Success, nil
	}

	p := &revisionphases.Phase{
		Client:   r.Client,
		Recorder: r.Recorder,
		Revision: revision,
	}
	result, reconcileResult, err := lifecycle.ExecuteSubRoutine(ctx, p.EnsureCreatePhases())
	if err != nil || reconcileResult == lifecycle.Failed {
		return result, reconcileResult, err
	}

	r.setReferencedCondition(revision)
	r.setReadyCondition(revision)

	revision.Status.Version = revision.Spec.Definition.Spec.Version
	now := new(metav1.Now())
	revision.Status.ResolvedAt = now

	return result, lifecycle.Success, nil
}

// EnsureDeleted implements lifecycle.Reconciler.
func (r *RevisionReconciler) EnsureDeleted(_ context.Context, _ lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	// Owned resources are cleaned up automatically via owner references; nothing extra to do here.
	return ctrl.Result{}, lifecycle.Success, nil
}

// setReferencedCondition counts environment finalizers and sets the Referenced condition.
func (r *RevisionReconciler) setReferencedCondition(revision *promotionv1alpha1.Revision) {
	count := 0
	for _, f := range revision.Finalizers {
		if strings.HasPrefix(f, promotionv1alpha1.EnvironmentFinalizerPrefix) {
			count++
		}
	}

	var c metav1.Condition
	if count > 0 {
		c = metav1.Condition{
			Type:    conditionReferenced,
			Status:  metav1.ConditionTrue,
			Reason:  "InUse",
			Message: fmt.Sprintf("Pinned by %d Environment(s); protected from pruning", count),
		}
	} else {
		c = metav1.Condition{
			Type:    conditionReferenced,
			Status:  metav1.ConditionFalse,
			Reason:  "Unreferenced",
			Message: "Not pinned by any Environment",
		}
	}
	c.ObservedGeneration = revision.Generation
	revision.SetCondition(c)
}

// setReadyCondition derives the Ready condition from HelmChartReady.
func (r *RevisionReconciler) setReadyCondition(revision *promotionv1alpha1.Revision) {
	helmChartReady := revision.GetConditionByType(conditionHelmChartReady)
	var c metav1.Condition
	if helmChartReady == nil || helmChartReady.Status != metav1.ConditionTrue {
		msg := "HelmChart not ready"
		if helmChartReady != nil {
			msg = helmChartReady.Message
		}
		c = metav1.Condition{
			Type:    string(kmeta.ReadyCondition),
			Status:  metav1.ConditionFalse,
			Reason:  "HelmChartNotReady",
			Message: msg,
		}
	} else {
		c = metav1.Condition{
			Type:    string(kmeta.ReadyCondition),
			Status:  metav1.ConditionTrue,
			Reason:  "Resolved",
			Message: "Revision resolved and chart ready",
		}
	}
	c.ObservedGeneration = revision.Generation
	revision.SetCondition(c)
}

// initializeConditions sets HelmChartReady and Referenced to Unknown at the start of each reconcile.
func (r *RevisionReconciler) initializeConditions(revision *promotionv1alpha1.Revision) {
	for _, t := range []string{conditionHelmChartReady, conditionReferenced} {
		revision.SetCondition(metav1.Condition{
			Type:               t,
			Status:             metav1.ConditionUnknown,
			Reason:             "Initializing",
			Message:            "Reconciliation in progress",
			ObservedGeneration: revision.Generation,
		})
	}
}
