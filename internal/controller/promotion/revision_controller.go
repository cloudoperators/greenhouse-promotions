// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package promotion

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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/flux"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
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

	if revision.Spec.Definition.Spec.HelmChart == nil {
		return ctrl.Result{}, lifecycle.Success, nil
	}

	helmRepo, err := r.ensureHelmRepository(ctx, revision)
	if err != nil {
		return ctrl.Result{}, lifecycle.Failed, err
	}

	if err := r.ensureHelmChart(ctx, revision, helmRepo); err != nil {
		return ctrl.Result{}, lifecycle.Failed, err
	}

	if err := r.ensureOrphanedHelmChartsDeleted(ctx, revision); err != nil {
		return ctrl.Result{}, lifecycle.Failed, err
	}

	return ctrl.Result{}, lifecycle.Success, nil
}

func (r *RevisionReconciler) EnsureDeleted(_ context.Context, _ lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	// Owned resources are cleaned up automatically via owner references; nothing extra to do here.
	return ctrl.Result{}, lifecycle.Success, nil
}

// ensureHelmRepository creates or updates the Flux HelmRepository owned by this Revision.
func (r *RevisionReconciler) ensureHelmRepository(ctx context.Context, revision *promotionv1alpha1.Revision) (*sourcev1.HelmRepository, error) {
	log := log.FromContext(ctx)
	repositoryURL := revision.Spec.Definition.Spec.HelmChart.Repository

	helmRepo := &sourcev1.HelmRepository{}
	helmRepo.Name = flux.ChartURLToName(repositoryURL)
	helmRepo.Namespace = revision.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmRepo, func() error {
		helmRepo.Spec.Type = flux.GetSourceRepositoryType(repositoryURL)
		helmRepo.Spec.URL = repositoryURL
		helmRepo.Spec.Interval = metav1.Duration{Duration: 24 * time.Hour}
		if flux.CheckIfLocalRegistry(repositoryURL) {
			helmRepo.Spec.Insecure = true
		}
		return controllerutil.SetControllerReference(revision, helmRepo, r.Scheme)
	})
	if err != nil {
		log.Error(err, "Failed to reconcile HelmRepository", "name", helmRepo.Name)
		return nil, err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		log.Info("Created HelmRepository", "name", helmRepo.Name)
		r.Recorder.Eventf(revision, nil, corev1.EventTypeNormal, "Created", "ReconcileRevision", "Created HelmRepository %s", helmRepo.Name)
	case controllerutil.OperationResultUpdated:
		log.Info("Updated HelmRepository", "name", helmRepo.Name)
		r.Recorder.Eventf(revision, nil, corev1.EventTypeNormal, "Updated", "ReconcileRevision", "Updated HelmRepository %s", helmRepo.Name)
	}

	return helmRepo, nil
}

// ensureHelmChart creates or updates the Flux HelmChart owned by this Revision and sets the HelmChartReady condition.
func (r *RevisionReconciler) ensureHelmChart(ctx context.Context, revision *promotionv1alpha1.Revision, helmRepo *sourcev1.HelmRepository) error {
	log := log.FromContext(ctx)
	helmChartSpec := revision.Spec.Definition.Spec.HelmChart

	helmChart := &sourcev1.HelmChart{}
	helmChart.Name = revision.FluxHelmChartResourceName()
	helmChart.Namespace = revision.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmChart, func() error {
		labels := helmChart.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[promotionv1alpha1.RevisionLabelSource] = revision.Name
		helmChart.SetLabels(labels)
		helmChart.Spec = sourcev1.HelmChartSpec{
			Chart:             helmChartSpec.Name,
			Version:           helmChartSpec.Version,
			Interval:          metav1.Duration{Duration: 24 * time.Hour},
			ReconcileStrategy: sourcev1.ReconcileStrategyChartVersion,
			SourceRef: sourcev1.LocalHelmChartSourceReference{
				Kind: sourcev1.HelmRepositoryKind,
				Name: helmRepo.Name,
			},
		}
		return controllerutil.SetControllerReference(revision, helmChart, r.Scheme)
	})
	if err != nil {
		log.Error(err, "Failed to reconcile HelmChart", "name", helmChart.Name)
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		log.Info("Created HelmChart", "name", helmChart.Name)
		r.Recorder.Eventf(revision, nil, corev1.EventTypeNormal, "Created", "ReconcileRevision", "Created HelmChart %s", helmChart.Name)
	case controllerutil.OperationResultUpdated:
		log.Info("Updated HelmChart", "name", helmChart.Name)
		r.Recorder.Eventf(revision, nil, corev1.EventTypeNormal, "Updated", "ReconcileRevision", "Updated HelmChart %s", helmChart.Name)
	}

	// Re-fetch to read current Flux status and set HelmChartReady condition.
	if err := r.Get(ctx, client.ObjectKeyFromObject(helmChart), helmChart); err != nil {
		return err
	}
	r.setHelmChartReadyCondition(revision, helmChart)
	return nil
}

// setHelmChartReadyCondition mirrors the Flux HelmChart Ready condition onto the Revision.
func (r *RevisionReconciler) setHelmChartReadyCondition(revision *promotionv1alpha1.Revision, helmChart *sourcev1.HelmChart) {
	readyCondition := meta.FindStatusCondition(helmChart.GetConditions(), fluxmeta.ReadyCondition)
	var c metav1.Condition
	switch {
	case readyCondition == nil:
		c = metav1.Condition{
			Type:    "HelmChartReady",
			Status:  metav1.ConditionUnknown,
			Reason:  "Pending",
			Message: "HelmChart status pending",
		}
	case readyCondition.Status == metav1.ConditionTrue:
		c = metav1.Condition{
			Type:    "HelmChartReady",
			Status:  metav1.ConditionTrue,
			Reason:  readyCondition.Reason,
			Message: readyCondition.Message,
		}
	default:
		c = metav1.Condition{
			Type:    "HelmChartReady",
			Status:  metav1.ConditionFalse,
			Reason:  readyCondition.Reason,
			Message: readyCondition.Message,
		}
	}
	c.ObservedGeneration = revision.Generation
	meta.SetStatusCondition(&revision.Status.Conditions, c)
}

// ensureOrphanedHelmChartsDeleted deletes HelmCharts labelled with this Revision's name
// whose name no longer matches the desired FluxHelmChartResourceName.
func (r *RevisionReconciler) ensureOrphanedHelmChartsDeleted(ctx context.Context, revision *promotionv1alpha1.Revision) error {
	log := log.FromContext(ctx)
	currentName := revision.FluxHelmChartResourceName()

	helmChartList := &sourcev1.HelmChartList{}
	if err := r.List(ctx, helmChartList,
		client.InNamespace(revision.Namespace),
		client.MatchingLabels{promotionv1alpha1.RevisionLabelSource: revision.Name},
	); err != nil {
		return fmt.Errorf("failed to list HelmCharts: %w", err)
	}

	for i := range helmChartList.Items {
		chart := &helmChartList.Items[i]
		controllerRef := metav1.GetControllerOf(chart)
		if controllerRef == nil || controllerRef.UID != revision.UID {
			continue
		}
		if chart.Name == currentName {
			continue
		}
		log.Info("Deleting orphaned HelmChart", "name", chart.Name)
		if err := r.Delete(ctx, chart); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to delete orphaned HelmChart %s: %w", chart.Name, err)
		}
		r.Recorder.Eventf(revision, nil, corev1.EventTypeNormal, "Deleted", "ReconcileRevision", "Deleted orphaned HelmChart %s", chart.Name)
	}
	return nil
}
