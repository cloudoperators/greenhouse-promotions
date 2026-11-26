// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
)

// RevisionValidator validates Revision resources.
type RevisionValidator struct{}

// +kubebuilder:webhook:path=/validate-promotion-cloudoperators-dev-v1alpha1-revision,mutating=false,failurePolicy=fail,sideEffects=None,groups=promotion.cloudoperators.dev,resources=revisions,verbs=update,versions=v1alpha1,name=vrevision.kb.io,admissionReviewVersions=v1

func SetupRevisionWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &promotionv1alpha1.Revision{}).
		WithValidator(&RevisionValidator{}).
		Complete()
}

func (v *RevisionValidator) ValidateCreate(_ context.Context, _ *promotionv1alpha1.Revision) (admission.Warnings, error) {
	return nil, nil
}

func (v *RevisionValidator) ValidateUpdate(_ context.Context, oldRevision, newRevision *promotionv1alpha1.Revision) (admission.Warnings, error) {
	if oldRevision.Spec.Ref != newRevision.Spec.Ref ||
		oldRevision.Spec.Digest != newRevision.Spec.Digest {
		return nil, apierrors.NewForbidden(
			newRevision.GroupVersionKind().GroupVersion().WithResource("revisions").GroupResource(),
			newRevision.Name,
			field.Forbidden(field.NewPath("spec"), "Revision spec is immutable"),
		)
	}
	return nil, nil
}

func (v *RevisionValidator) ValidateDelete(_ context.Context, _ *promotionv1alpha1.Revision) (admission.Warnings, error) {
	return nil, nil
}
