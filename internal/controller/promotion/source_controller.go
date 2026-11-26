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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
	sourcephase "github.com/cloudoperators/greenhouse-promotions/internal/phases/source"
)

// SourceReconciler reconciles a Source object
type SourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=sources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=sources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=sources/finalizers,verbs=update
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=revisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Source object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *SourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return lifecycle.Reconcile(ctx, r.Client, req.NamespacedName, &promotionv1alpha1.Source{}, r)
}

func (r *SourceReconciler) EnsureCreated(ctx context.Context, obj lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	src := obj.(*promotionv1alpha1.Source)
	phase := &sourcephase.Phase{
		Client: r.Client,
		Scheme: r.Scheme,
	}
	routines := []lifecycle.SubRoutine{
		phase.EnsureClient(src),
		phase.EnsureDiscovered(src),
		phase.EnsureRevisions(src),
		phase.EnsureStatus(src),
	}
	return lifecycle.ExecuteSubRoutine(ctx, routines)
}

func (r *SourceReconciler) EnsureDeleted(ctx context.Context, obj lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	log := ctrl.LoggerFrom(ctx)
	source := obj.(*promotionv1alpha1.Source)
	log.Info("ensure source", "namespace", source.GetNamespace(), "name", source.GetName())
	return ctrl.Result{}, lifecycle.Success, nil
}

func (r *SourceReconciler) GetEventRecorder() events.EventRecorder {
	return r.recorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *SourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorder("source-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&promotionv1alpha1.Source{}).
		Owns(&promotionv1alpha1.Revision{}).
		Named("promotion-source").
		Complete(r)
}
