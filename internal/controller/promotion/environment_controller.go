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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
)

// EnvironmentReconciler reconciles a Environment object
type EnvironmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=environments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=environments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=environments/finalizers,verbs=update
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=sources,verbs=get;list;watch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=promotions,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=revisions,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=greenhouse.sap,resources=plugins,verbs=get;list;watch
// +kubebuilder:rbac:groups=greenhouse.sap,resources=clusters,verbs=get;list;watch

// Reconcile is the Environment reconcile loop. It is intentionally a no-op for now:
// the controller is wired and runnable, but the reconcile logic (resolve the target
// Revision from spec.source, pin it, open a Promotion, run the gate) lands in a
// follow-up once the Revision and Promotion types exist.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	var env promotionv1alpha1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// TODO: implement the reconcile logic (target Revision resolution, Promotion
	// creation, gate) once the Revision and Promotion types are available.
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&promotionv1alpha1.Environment{}).
		// TODO: add the spec's watches as their target types land.
		//   .Watches(&promotionv1alpha1.Source{}, ...)      // newestFromSource: enqueue heads for spec.sourceRef
		//   .Watches(&promotionv1alpha1.Revision{}, ...)     // enqueue the Environment pinning this Revision
		//   .Owns(&promotionv1alpha1.Promotion{})            // the Environment creates & owns a Promotion;
		//                                                    // its Ready condition is the readiness signal
		//                                                    // (Promotion type is owned by another team)
		//   .Watches(&promotionv1alpha1.Environment{}, ...)  // verifiedIn: enqueue downstream Environments
		// Note: the Environment does NOT watch Plugins directly — Plugin-level
		// convergence is the Promotion controller's concern; the Environment only
		// watches the Promotion it owns.
		Named("promotion-environment").
		Complete(r)
}
