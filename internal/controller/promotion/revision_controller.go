// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package promotion

import (
	"context"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
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

// SetupWithManager sets up the controller with the Manager.
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

// Reconcile is the main reconciliation loop for a Revision.
func (r *RevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	revision := &promotionv1alpha1.Revision{}
	if err := r.Get(ctx, req.NamespacedName, revision); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}
