// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// Package revision holds the reconcile phases for the Revision controller.
package revision

import (
	"context"
	"fmt"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/flux"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
)

const (
	conditionHelmChartReady = "HelmChartReady"
)

// Phase holds the per-reconcile state shared across a Revision's subroutine chain.
type Phase struct {
	Client   client.Client
	Recorder events.EventRecorder
	Revision *promotionv1alpha1.Revision

	// helmRepo is set by EnsureHelmRepository and consumed by EnsureHelmChart.
	helmRepo *sourcev1.HelmRepository
}

// EnsureCreatePhases returns the ordered subroutines for a create/update reconcile.
func (p *Phase) EnsureCreatePhases() []lifecycle.SubRoutine {
	return []lifecycle.SubRoutine{
		p.EnsureHelmRepository,
		p.EnsureHelmChart,
		p.EnsureOrphanedHelmChartsDeleted,
	}
}

// EnsureHelmRepository creates or updates the Flux HelmRepository owned by this Revision.
func (p *Phase) EnsureHelmRepository(ctx context.Context) (lifecycle.Result, error) {
	log := ctrllog.FromContext(ctx)
	repositoryURL := p.Revision.Spec.Definition.Spec.HelmChart.Repository

	source, err := p.getOwningSource(ctx)
	if err != nil {
		log.Error(err, "Failed to get owning Source")
		return lifecycle.Break(), err
	}

	helmRepo := &sourcev1.HelmRepository{}
	helmRepo.Name = flux.ChartURLToName(repositoryURL)
	helmRepo.Namespace = p.Revision.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, p.Client, helmRepo, func() error {
		helmRepo.Spec.Type = flux.GetSourceRepositoryType(repositoryURL)
		helmRepo.Spec.URL = repositoryURL
		helmRepo.Spec.Interval = metav1.Duration{Duration: 24 * time.Hour}
		if flux.CheckIfLocalRegistry(repositoryURL) {
			helmRepo.Spec.Insecure = true
		}
		if source != nil && source.Spec.Source != nil && source.Spec.Source.Git != nil && source.Spec.Source.Git.SecretRef != nil {
			helmRepo.Spec.SecretRef = &fluxmeta.LocalObjectReference{Name: source.Spec.Source.Git.SecretRef.Name}
		}
		if source != nil && source.Spec.Source != nil && source.Spec.Source.Oci != nil && source.Spec.Source.Oci.SecretRef != nil {
			helmRepo.Spec.SecretRef = &fluxmeta.LocalObjectReference{Name: source.Spec.Source.Oci.SecretRef.Name}
		}
		return controllerutil.SetControllerReference(p.Revision, helmRepo, p.Client.Scheme())
	})
	if err != nil {
		log.Error(err, "Failed to reconcile HelmRepository", "name", helmRepo.Name)
		return lifecycle.Break(), err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		log.Info("Created HelmRepository", "name", helmRepo.Name)
		p.Recorder.Eventf(p.Revision, nil, corev1.EventTypeNormal, "Created", "ReconcileRevision", "Created HelmRepository %s", helmRepo.Name)
	case controllerutil.OperationResultUpdated:
		log.Info("Updated HelmRepository", "name", helmRepo.Name)
		p.Recorder.Eventf(p.Revision, nil, corev1.EventTypeNormal, "Updated", "ReconcileRevision", "Updated HelmRepository %s", helmRepo.Name)
	}

	p.helmRepo = helmRepo
	return lifecycle.Continue(), nil
}

// EnsureHelmChart creates or updates the Flux HelmChart owned by this Revision and sets the HelmChartReady condition.
func (p *Phase) EnsureHelmChart(ctx context.Context) (lifecycle.Result, error) {
	log := ctrllog.FromContext(ctx)
	helmChartSpec := p.Revision.Spec.Definition.Spec.HelmChart

	helmChart := &sourcev1.HelmChart{}
	helmChart.Name = p.Revision.FluxHelmChartResourceName()
	helmChart.Namespace = p.Revision.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, p.Client, helmChart, func() error {
		labels := helmChart.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[promotionv1alpha1.RevisionLabelSource] = p.Revision.Name
		helmChart.SetLabels(labels)
		helmChart.Spec = sourcev1.HelmChartSpec{
			Chart:             helmChartSpec.Name,
			Version:           helmChartSpec.Version,
			Interval:          metav1.Duration{Duration: 24 * time.Hour},
			ReconcileStrategy: sourcev1.ReconcileStrategyChartVersion,
			SourceRef: sourcev1.LocalHelmChartSourceReference{
				Kind: sourcev1.HelmRepositoryKind,
				Name: p.helmRepo.Name,
			},
		}
		return controllerutil.SetControllerReference(p.Revision, helmChart, p.Client.Scheme())
	})
	if err != nil {
		log.Error(err, "Failed to reconcile HelmChart", "name", helmChart.Name)
		return lifecycle.Break(), err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		log.Info("Created HelmChart", "name", helmChart.Name)
		p.Recorder.Eventf(p.Revision, nil, corev1.EventTypeNormal, "Created", "ReconcileRevision", "Created HelmChart %s", helmChart.Name)
	case controllerutil.OperationResultUpdated:
		log.Info("Updated HelmChart", "name", helmChart.Name)
		p.Recorder.Eventf(p.Revision, nil, corev1.EventTypeNormal, "Updated", "ReconcileRevision", "Updated HelmChart %s", helmChart.Name)
	}

	// Re-fetch to read current Flux status and set HelmChartReady condition.
	if err := p.Client.Get(ctx, client.ObjectKeyFromObject(helmChart), helmChart); err != nil {
		return lifecycle.Break(), err
	}
	p.setHelmChartReadyCondition(helmChart)
	return lifecycle.Continue(), nil
}

// EnsureOrphanedHelmChartsDeleted deletes HelmCharts labelled with this Revision's name
// whose name no longer matches the desired FluxHelmChartResourceName.
func (p *Phase) EnsureOrphanedHelmChartsDeleted(ctx context.Context) (lifecycle.Result, error) {
	log := ctrllog.FromContext(ctx)
	currentName := p.Revision.FluxHelmChartResourceName()

	helmChartList := &sourcev1.HelmChartList{}
	if err := p.Client.List(ctx, helmChartList,
		client.InNamespace(p.Revision.Namespace),
		client.MatchingLabels{promotionv1alpha1.RevisionLabelSource: p.Revision.Name},
	); err != nil {
		return lifecycle.Break(), fmt.Errorf("failed to list HelmCharts: %w", err)
	}

	for i := range helmChartList.Items {
		chart := &helmChartList.Items[i]
		controllerRef := metav1.GetControllerOf(chart)
		if controllerRef == nil || controllerRef.UID != p.Revision.UID {
			continue
		}
		if chart.Name == currentName {
			continue
		}
		log.Info("Deleting orphaned HelmChart", "name", chart.Name)
		if err := p.Client.Delete(ctx, chart); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return lifecycle.Break(), fmt.Errorf("failed to delete orphaned HelmChart %s: %w", chart.Name, err)
		}
		p.Recorder.Eventf(p.Revision, nil, corev1.EventTypeNormal, "Deleted", "ReconcileRevision", "Deleted orphaned HelmChart %s", chart.Name)
	}
	return lifecycle.Continue(), nil
}

// getOwningSource returns the Source that owns this Revision via ownerReference, or nil if not found.
func (p *Phase) getOwningSource(ctx context.Context) (*promotionv1alpha1.Source, error) {
	for _, ref := range p.Revision.OwnerReferences {
		if ref.Kind != "Source" {
			continue
		}
		source := &promotionv1alpha1.Source{}
		if err := p.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: p.Revision.Namespace}, source); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return source, nil
	}
	return nil, nil
}

// setHelmChartReadyCondition mirrors the Flux HelmChart Ready condition onto the Revision.
func (p *Phase) setHelmChartReadyCondition(helmChart *sourcev1.HelmChart) {
	readyCondition := meta.FindStatusCondition(helmChart.GetConditions(), fluxmeta.ReadyCondition)
	var c metav1.Condition
	switch {
	case readyCondition == nil:
		c = metav1.Condition{
			Type:    conditionHelmChartReady,
			Status:  metav1.ConditionUnknown,
			Reason:  "Pending",
			Message: "HelmChart status pending",
		}
	case readyCondition.Status == metav1.ConditionTrue:
		c = metav1.Condition{
			Type:    conditionHelmChartReady,
			Status:  metav1.ConditionTrue,
			Reason:  readyCondition.Reason,
			Message: readyCondition.Message,
		}
	default:
		c = metav1.Condition{
			Type:    conditionHelmChartReady,
			Status:  metav1.ConditionFalse,
			Reason:  readyCondition.Reason,
			Message: readyCondition.Message,
		}
	}
	c.ObservedGeneration = p.Revision.Generation
	meta.SetStatusCondition(&p.Revision.Status.Conditions, c)
}
