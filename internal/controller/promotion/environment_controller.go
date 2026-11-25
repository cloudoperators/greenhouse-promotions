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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kmeta "github.com/cloudoperators/greenhouse-promotions/api/meta"
	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
)

// Condition reasons specific to the Environment controller.
const (
	reasonNoRevision          = "NoRevision"
	reasonRevisionNotReady    = "RevisionNotReady"
	reasonUpstreamNotFound    = "UpstreamNotFound"
	reasonUpstreamNotVerified = "UpstreamNotVerified"
	reasonInvalidSource       = "InvalidSource"
	reasonNoTargetPlugins     = "NoTargetPlugins"
	reasonRolloutPending      = "RolloutPending"
	reasonSoaking             = "Soaking"
	reasonSoakPassed          = "SoakPassed"
	reasonConverged           = "Converged"
)

// EnvironmentReconciler reconciles an Environment object.
type EnvironmentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// GetEventRecorder implements lifecycle.Reconciler.
func (r *EnvironmentReconciler) GetEventRecorder() events.EventRecorder {
	return r.Recorder
}

// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=environments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=environments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=environments/finalizers,verbs=update
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=sources,verbs=get;list;watch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=rollouts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=promotion.cloudoperators.dev,resources=revisions,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=greenhouse.sap,resources=plugins,verbs=get;list;watch
// +kubebuilder:rbac:groups=greenhouse.sap,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile delegates to the lifecycle harness, which handles fetch, the
// cleanup finalizer, create/delete routing, and status patching.
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return lifecycle.Reconcile(ctx, r.Client, req.NamespacedName, &promotionv1alpha1.Environment{}, r)
}

// EnsureCreated runs the create/update reconcile chain for an Environment.
func (r *EnvironmentReconciler) EnsureCreated(ctx context.Context, obj lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	env, ok := obj.(*promotionv1alpha1.Environment)
	if !ok {
		return ctrl.Result{}, lifecycle.Failed, fmt.Errorf("expected *Environment, got %T", obj)
	}

	env.Status.ObservedGeneration = env.Generation

	return lifecycle.ExecuteSubRoutine(ctx, []lifecycle.SubRoutine{
		func(ctx context.Context) (lifecycle.Result, error) { return r.resolveTargetRevision(ctx, env) },
		func(ctx context.Context) (lifecycle.Result, error) { return r.pinRevision(ctx, env) },
		func(ctx context.Context) (lifecycle.Result, error) { return r.ensureRollout(ctx, env) },
		func(ctx context.Context) (lifecycle.Result, error) { return r.observeRolloutAndGate(ctx, env) },
		func(ctx context.Context) (lifecycle.Result, error) { return r.appendHistory(ctx, env) },
	})
}

// EnsureDeleted unpins the target Revision so it can be garbage-collected.
func (r *EnvironmentReconciler) EnsureDeleted(ctx context.Context, obj lifecycle.RuntimeObject) (ctrl.Result, lifecycle.ReconcileResult, error) {
	env, ok := obj.(*promotionv1alpha1.Environment)
	if !ok {
		return ctrl.Result{}, lifecycle.Failed, fmt.Errorf("expected *Environment, got %T", obj)
	}
	if env.Status.CurrentRevision != nil {
		if err := r.unpinRevision(ctx, env, env.Status.CurrentRevision.Name); err != nil {
			return ctrl.Result{}, lifecycle.Failed, err
		}
	}
	return ctrl.Result{}, lifecycle.Success, nil
}

// revisionFinalizer is the per-Environment finalizer placed on a pinned Revision.
func (r *EnvironmentReconciler) revisionFinalizer(env *promotionv1alpha1.Environment) string {
	return promotionv1alpha1.EnvironmentFinalizerPrefix + env.Name
}

// resolveTargetRevision selects the Revision this Environment should run, writing
// it to status.currentRevision. It branches on the spec.source one-of.
func (r *EnvironmentReconciler) resolveTargetRevision(ctx context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	fromSource := env.Spec.Source.RequestFromSource != nil
	verified := env.Spec.Source.VerifiedIn != nil
	switch {
	case fromSource == verified:
		// CRD enforces exactly-one, but guard defensively.
		r.setReadyFalse(env, reasonInvalidSource,
			"exactly one of spec.source.requestFromSource or verifiedIn must be set")
		return lifecycle.Break(), nil
	case fromSource:
		return r.resolveRequestFromSource(ctx, env)
	default:
		return r.resolveVerifiedIn(ctx, env)
	}
}

// resolveRequestFromSource selects the target Revision from the Source's
// status.revisions, which is ordered oldest-first (newest last) and is the
// authoritative version order (we do not re-derive it). Latest always tracks the
// newest ready Revision. Sequential starts a fresh Environment at the oldest ready
// Revision and then advances one version at a time, holding on the current Revision
// until it is Verified before stepping to the next ready entry above it.
func (r *EnvironmentReconciler) resolveRequestFromSource(ctx context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	var source promotionv1alpha1.Source
	if err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: env.Spec.SourceRef.Name}, &source); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return lifecycle.Break(), err
		}
		r.setReadyFalse(env, reasonNoRevision,
			fmt.Sprintf("Source %q not found", env.Spec.SourceRef.Name))
		return lifecycle.Requeue(), nil
	}

	// Source.status.revisions is ordered oldest-first (newest last) and carries a
	// per-entry Ready flag, so we select by list position + readiness and never
	// re-derive version order ourselves.
	history := source.Status.Revisions
	if len(history) == 0 {
		r.setReadyFalse(env, reasonNoRevision,
			fmt.Sprintf("Source %q has not produced a Revision yet", env.Spec.SourceRef.Name))
		return lifecycle.Requeue(), nil
	}

	// Latest always tracks the newest ready Revision, including on first adoption.
	if env.Spec.Source.RequestFromSource.Strategy == promotionv1alpha1.StrategyLatest {
		return r.adoptRevision(env, newestReady(history))
	}

	// Sequential: a fresh Environment starts at the oldest ready Revision.
	if env.Status.CurrentRevision == nil {
		return r.adoptRevision(env, oldestReady(history))
	}

	// Hold on the current Revision until it is Verified, then step to the next ready
	// entry above it (oldest-first order -> toward a higher index).
	if !currentIsVerified(env) {
		// Keep running the current Revision; the gate subroutine drives Verified.
		return lifecycle.Continue(), nil
	}
	return r.adoptRevision(env, nextReady(history, env.Status.CurrentRevision))
}

// adoptRevision sets status.currentRevision to the given Source history entry, or
// holds with an explanatory condition when nil/not ready.
func (r *EnvironmentReconciler) adoptRevision(env *promotionv1alpha1.Environment, rev *promotionv1alpha1.SourceRevision) (lifecycle.Result, error) {
	if rev == nil {
		// No eligible next/newest entry — either nothing ready, or already on the
		// newest. Stay put; nothing to advance to.
		if env.Status.CurrentRevision == nil {
			r.setReadyFalse(env, reasonNoRevision, "no ready Revision available to roll out")
			return lifecycle.Requeue(), nil
		}
		return lifecycle.Continue(), nil
	}
	if !rev.Ready {
		r.setReadyFalse(env, reasonRevisionNotReady,
			fmt.Sprintf("Revision %q is not ready", rev.Name))
		return lifecycle.Requeue(), nil
	}
	env.Status.CurrentRevision = &promotionv1alpha1.RevisionReference{Name: rev.Name}
	return lifecycle.Continue(), nil
}

// resolveVerifiedIn takes the upstream Environment's verified Revision, gated on
// the upstream's Verified condition.
func (r *EnvironmentReconciler) resolveVerifiedIn(ctx context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	upstreamName := env.Spec.Source.VerifiedIn.Name
	var upstream promotionv1alpha1.Environment
	if err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: upstreamName}, &upstream); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return lifecycle.Break(), err
		}
		r.setReadyFalse(env, reasonUpstreamNotFound,
			fmt.Sprintf("upstream Environment %q not found", upstreamName))
		return lifecycle.Requeue(), nil
	}

	verifiedCond := kmeta.GetConditionByType(upstream.Status.Conditions, promotionv1alpha1.ConditionVerified)
	if upstream.Status.VerifiedRevision == nil || verifiedCond == nil || verifiedCond.Status != metav1.ConditionTrue {
		r.setReadyFalse(env, reasonUpstreamNotVerified,
			fmt.Sprintf("upstream Environment %q has not verified a Revision yet", upstreamName))
		return lifecycle.Requeue(), nil
	}

	env.Status.CurrentRevision = &promotionv1alpha1.RevisionReference{Name: upstream.Status.VerifiedRevision.Name}
	return lifecycle.Continue(), nil
}

// pinRevision adds this Environment's finalizer to the target Revision and
// removes it from any previously-pinned Revision whose target changed.
func (r *EnvironmentReconciler) pinRevision(ctx context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	target := env.Status.CurrentRevision
	if target == nil {
		return lifecycle.Continue(), nil
	}

	var rev promotionv1alpha1.Revision
	if err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: target.Name}, &rev); err != nil {
		return lifecycle.Break(), err
	}
	if controllerutil.AddFinalizer(&rev, r.revisionFinalizer(env)) {
		if err := r.Update(ctx, &rev); err != nil {
			return lifecycle.Break(), err
		}
	}
	return lifecycle.Continue(), nil
}

// unpinRevision removes this Environment's finalizer from the named Revision.
func (r *EnvironmentReconciler) unpinRevision(ctx context.Context, env *promotionv1alpha1.Environment, name string) error {
	var rev promotionv1alpha1.Revision
	if err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: name}, &rev); err != nil {
		return client.IgnoreNotFound(err)
	}
	if controllerutil.RemoveFinalizer(&rev, r.revisionFinalizer(env)) {
		return r.Update(ctx, &rev)
	}
	return nil
}

// resolveTargetPlugins resolves spec.clusterSelector -> greenhouse Clusters ->
// Plugins. A Plugin is a target when it carries the labels greenhouse.sap/cluster
// (one of the selected clusters) and greenhouse.sap/plugindefinition (the Source's
// definition). Returns the Plugins as Rollout PluginReferences.
//
// NOTE: the PluginDefinition label is assumed equal to spec.sourceRef.Name for now;
// in the future the Source will record the resolved PluginDefinition name directly.
func (r *EnvironmentReconciler) resolveTargetPlugins(ctx context.Context, env *promotionv1alpha1.Environment) ([]promotionv1alpha1.PluginReference, error) {
	sel, err := metav1.LabelSelectorAsSelector(&env.Spec.ClusterSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid clusterSelector: %w", err)
	}

	var clusters greenhousev1alpha1.ClusterList
	if err := r.List(ctx, &clusters,
		client.InNamespace(env.Namespace),
		client.MatchingLabelsSelector{Selector: sel},
	); err != nil {
		return nil, err
	}

	pdName := env.Spec.SourceRef.Name // see NOTE above
	var refs []promotionv1alpha1.PluginReference
	for i := range clusters.Items {
		clusterName := clusters.Items[i].Name
		var plugins greenhousev1alpha1.PluginList
		if err := r.List(ctx, &plugins,
			client.InNamespace(env.Namespace),
			client.MatchingLabels{
				greenhouseapis.LabelKeyCluster:          clusterName,
				greenhouseapis.LabelKeyPluginDefinition: pdName,
			},
		); err != nil {
			return nil, err
		}
		for j := range plugins.Items {
			refs = append(refs, promotionv1alpha1.PluginReference{Name: plugins.Items[j].Name})
		}
	}
	return refs, nil
}

// ensureRollout resolves the target Plugins and drives a single Rollout owned by
// this Environment (named after the Environment) towards the current target
// Revision. The Rollout spec is mutable: on every reconcile the desired
// revisionRef and pluginRefs are written via CreateOrUpdate, so a new target
// Revision or a changed fleet (clusters joining/leaving the selector, Plugins
// appearing/disappearing) is reconciled in place by the Rollout controller rather
// than spawning a new Rollout. Because the name is derived from the Environment and
// the desired spec is recomputed from scratch each pass, the operation is
// idempotent and crash-safe.
//
// If resolution returns no Plugins, ensureRollout keeps any Rollout that already
// exists (so a momentary empty resolution does not tear it down) and only holds at
// NoTargetPlugins when there is no Rollout to work with at all.
func (r *EnvironmentReconciler) ensureRollout(ctx context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	target := env.Status.CurrentRevision
	if target == nil {
		return lifecycle.Continue(), nil
	}

	pluginRefs, err := r.resolveTargetPlugins(ctx, env)
	if err != nil {
		return lifecycle.Break(), err
	}
	if len(pluginRefs) == 0 {
		// Resolution produced no target Plugins (clusters not matched, Plugins not
		// created, or a transient empty list). If a Rollout already exists, keep it
		// and let the observe/gate step run against it rather than tearing it down on
		// a momentary empty resolution; a Plugin/Cluster change re-triggers reconcile
		// and refreshes its pluginRefs. Only when there is nothing to work with at all
		// do we hold — a Rollout with empty pluginRefs would violate the CRD's
		// MinItems=1, so we never write one.
		var existing promotionv1alpha1.Rollout
		switch err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: rolloutName(env)}, &existing); {
		case err == nil:
			return lifecycle.Continue(), nil
		case client.IgnoreNotFound(err) != nil:
			return lifecycle.Break(), err
		default:
			r.setReadyFalse(env, reasonNoTargetPlugins,
				"clusterSelector resolved no target Plugins for the Source's definition")
			return lifecycle.Requeue(), nil
		}
	}

	rollout := &promotionv1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: rolloutName(env), Namespace: env.Namespace},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, rollout, func() error {
		rollout.Spec.RevisionRef = promotionv1alpha1.RevisionReference{Name: target.Name}
		rollout.Spec.PluginRefs = pluginRefs
		return controllerutil.SetControllerReference(env, rollout, r.Scheme)
	}); err != nil {
		return lifecycle.Break(), err
	}
	return lifecycle.Continue(), nil
}

// observeRolloutAndGate reflects the owned Rollout's readiness into status.targets
// and runs the soak gate: once the Rollout's Ready condition has held for
// spec.gate.soak, it sets Healthy + Verified + verifiedRevision.
func (r *EnvironmentReconciler) observeRolloutAndGate(ctx context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	target := env.Status.CurrentRevision
	if target == nil {
		return lifecycle.Continue(), nil
	}

	var rollout promotionv1alpha1.Rollout
	if err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: rolloutName(env)}, &rollout); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return lifecycle.Break(), err
		}
		r.setReadyFalse(env, reasonRolloutPending, "Rollout has not been created yet")
		return lifecycle.Requeue(), nil
	}

	if rollout.Status.Targets != nil {
		env.Status.Targets = promotionv1alpha1.EnvironmentTargets{
			Total: rollout.Status.Targets.Selected,
			Ready: rollout.Status.Targets.Converged,
		}
	}

	readyCond := kmeta.GetConditionByType(rollout.Status.Conditions, promotionv1alpha1.RolloutReadyCondition)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		env.SetCondition(metav1.Condition{
			Type: promotionv1alpha1.ConditionHealthy, Status: metav1.ConditionFalse,
			Reason: reasonRolloutPending, Message: "Rollout is not ready",
		})
		return lifecycle.Requeue(), nil
	}

	// Rollout is Ready; the cohort is healthy. Run the soak from the Ready
	// condition's LastTransitionTime.
	env.SetCondition(metav1.Condition{
		Type: promotionv1alpha1.ConditionHealthy, Status: metav1.ConditionTrue,
		Reason: reasonConverged, Message: "Rollout converged and is ready",
	})

	elapsed := time.Since(readyCond.LastTransitionTime.Time)
	soak := env.Spec.Gate.Soak.Duration
	if elapsed < soak {
		env.SetCondition(metav1.Condition{
			Type: promotionv1alpha1.ConditionVerified, Status: metav1.ConditionFalse,
			Reason:  reasonSoaking,
			Message: fmt.Sprintf("soaking: %s of %s elapsed", elapsed.Round(time.Second), soak),
		})
		return lifecycle.RequeueAfter(soak - elapsed), nil
	}

	env.Status.VerifiedRevision = &promotionv1alpha1.RevisionReference{Name: target.Name}
	env.SetCondition(metav1.Condition{
		Type: promotionv1alpha1.ConditionVerified, Status: metav1.ConditionTrue,
		Reason:  reasonSoakPassed,
		Message: fmt.Sprintf("healthy through %s soak; Revision %q verified", soak, target.Name),
	})
	return lifecycle.Continue(), nil
}

// appendHistory records a verified rollout in status.history (newest-first),
// trimmed to spec.maxHistory. Idempotent: it only prepends when the newest entry
// is not already this verified Revision.
func (r *EnvironmentReconciler) appendHistory(_ context.Context, env *promotionv1alpha1.Environment) (lifecycle.Result, error) {
	if env.Status.VerifiedRevision == nil {
		return lifecycle.Continue(), nil
	}
	rev := env.Status.VerifiedRevision.Name
	if len(env.Status.History) > 0 && env.Status.History[0].Revision != nil &&
		env.Status.History[0].Revision.Name == rev {
		return lifecycle.Continue(), nil
	}

	now := metav1.Now()
	entry := promotionv1alpha1.HistoryEntry{
		Revision:   &promotionv1alpha1.RevisionReference{Name: rev},
		EnteredAt:  now,
		VerifiedAt: &now,
		Result:     promotionv1alpha1.HistoryResultVerified,
	}
	env.Status.History = append([]promotionv1alpha1.HistoryEntry{entry}, env.Status.History...)
	if max := int(env.Spec.MaxHistory); max > 0 && len(env.Status.History) > max {
		env.Status.History = env.Status.History[:max]
	}
	return lifecycle.Continue(), nil
}

// setReadyFalse sets the Ready condition to False with the given reason/message.
// The lifecycle harness will not overwrite a Ready condition whose reason is not
// its own PendingCreation placeholder.
func (r *EnvironmentReconciler) setReadyFalse(env *promotionv1alpha1.Environment, reason, msg string) {
	env.SetCondition(metav1.Condition{
		Type:    promotionv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
}

// rolloutName derives the stable Rollout name for an Environment. There is one
// Rollout per Environment; its spec is reconciled in place as the target Revision
// or the resolved Plugins change.
func rolloutName(env *promotionv1alpha1.Environment) string {
	return env.Name
}

// Source.status.revisions is ordered ascending (OLDEST-first); the newest Revision
// is the last element (and is also named by Source.status.latestRevisionName). The
// helpers below index against that contract.

// newestReady returns the newest ready entry (scan from the end), or nil if none is
// ready.
func newestReady(history []promotionv1alpha1.SourceRevision) *promotionv1alpha1.SourceRevision {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Ready {
			return &history[i]
		}
	}
	return nil
}

// oldestReady returns the oldest ready entry (scan from the start), or nil if none is
// ready. Used by the Sequential strategy to start a fresh Environment at the bottom
// of the
// retained history.
func oldestReady(history []promotionv1alpha1.SourceRevision) *promotionv1alpha1.SourceRevision {
	for i := range history {
		if history[i].Ready {
			return &history[i]
		}
	}
	return nil
}

// nextReady returns the next ready entry above cur — i.e. the nearest ready entry
// strictly newer than cur. If cur is nil it returns the oldest ready entry (so a
// fresh Environment starts at the bottom and steps up). Returns nil when nothing
// qualifies (already on the newest ready entry, or cur is not present in the window).
func nextReady(history []promotionv1alpha1.SourceRevision, cur *promotionv1alpha1.RevisionReference) *promotionv1alpha1.SourceRevision {
	if cur == nil {
		return oldestReady(history)
	}

	curIdx := -1
	for i := range history {
		if history[i].Name == cur.Name {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		// cur aged out of the window. The cleanup job keeps in-use Revisions in
		// history, so this is not expected; hold rather than silently skip versions.
		return nil
	}
	// Oldest-first: entries strictly newer than cur sit at HIGHER indices. Step to
	// the nearest ready one.
	for i := curIdx + 1; i < len(history); i++ {
		if history[i].Ready {
			return &history[i]
		}
	}
	return nil
}

// currentIsVerified reports whether the Environment's current Revision has been
// verified by its gate (status.verifiedRevision matches status.currentRevision).
func currentIsVerified(env *promotionv1alpha1.Environment) bool {
	return env.Status.CurrentRevision != nil &&
		env.Status.VerifiedRevision != nil &&
		env.Status.VerifiedRevision.Name == env.Status.CurrentRevision.Name
}

// SetupWithManager wires the Environment controller.
func (r *EnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.Recorder = mgr.GetEventRecorder("promotion-environment")

	return ctrl.NewControllerManagedBy(mgr).
		Named("promotion-environment").
		For(&promotionv1alpha1.Environment{}, builder.WithPredicates(lifecycle.IgnoreStatusUpdatePredicate())).
		Owns(&promotionv1alpha1.Rollout{}).
		Watches(&promotionv1alpha1.Source{}, handler.EnqueueRequestsFromMapFunc(r.environmentsForSource)).
		Watches(&promotionv1alpha1.Environment{}, handler.EnqueueRequestsFromMapFunc(r.downstreamsForEnvironment)).
		// A Cluster joining or leaving an Environment's clusterSelector, or a Plugin
		// appearing/disappearing on a selected Cluster, changes the resolved target
		// Plugins and so the desired Rollout. Re-enqueue the affected Environments so
		// the single owned Rollout is reconciled in place.
		Watches(&greenhousev1alpha1.Cluster{}, handler.EnqueueRequestsFromMapFunc(r.environmentsForCluster)).
		Watches(&greenhousev1alpha1.Plugin{}, handler.EnqueueRequestsFromMapFunc(r.environmentsForPlugin)).
		Complete(r)
}

// environmentsForSource enqueues head-of-chain Environments that reference the
// changed Source.
func (r *EnvironmentReconciler) environmentsForSource(ctx context.Context, obj client.Object) []reconcile.Request {
	var envs promotionv1alpha1.EnvironmentList
	if err := r.List(ctx, &envs, client.InNamespace(obj.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list Environments for Source", "source", obj.GetName())
		return nil
	}
	var reqs []reconcile.Request
	for i := range envs.Items {
		e := &envs.Items[i]
		if e.Spec.Source.RequestFromSource != nil && e.Spec.SourceRef.Name == obj.GetName() {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(e)})
		}
	}
	return reqs
}

// downstreamsForEnvironment enqueues Environments gated behind the changed
// (upstream) Environment via verifiedIn.
func (r *EnvironmentReconciler) downstreamsForEnvironment(ctx context.Context, obj client.Object) []reconcile.Request {
	var envs promotionv1alpha1.EnvironmentList
	if err := r.List(ctx, &envs, client.InNamespace(obj.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list Environments for upstream", "upstream", obj.GetName())
		return nil
	}
	var reqs []reconcile.Request
	for i := range envs.Items {
		e := &envs.Items[i]
		if e.Spec.Source.VerifiedIn != nil && e.Spec.Source.VerifiedIn.Name == obj.GetName() {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(e)})
		}
	}
	return reqs
}

// environmentsForCluster enqueues every Environment whose clusterSelector matches
// the changed Cluster's labels. A Cluster moving in or out of a selector changes
// the resolved target Plugins, so the owning Environment's Rollout must be
// reconciled.
func (r *EnvironmentReconciler) environmentsForCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	var envs promotionv1alpha1.EnvironmentList
	if err := r.List(ctx, &envs, client.InNamespace(obj.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list Environments for Cluster", "cluster", obj.GetName())
		return nil
	}
	clusterLabels := labels.Set(obj.GetLabels())
	var reqs []reconcile.Request
	for i := range envs.Items {
		e := &envs.Items[i]
		sel, err := metav1.LabelSelectorAsSelector(&e.Spec.ClusterSelector)
		if err != nil {
			continue
		}
		if sel.Matches(clusterLabels) {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(e)})
		}
	}
	return reqs
}

// environmentsForPlugin enqueues every Environment whose Source owns the changed
// Plugin's PluginDefinition. The precise cluster-match is re-checked in
// resolveTargetPlugins; this only narrows the fan-out by definition.
//
// NOTE: like resolveTargetPlugins, this assumes the PluginDefinition label equals
// the Environment's spec.sourceRef.Name for now.
func (r *EnvironmentReconciler) environmentsForPlugin(ctx context.Context, obj client.Object) []reconcile.Request {
	pdName := obj.GetLabels()[greenhouseapis.LabelKeyPluginDefinition]
	if pdName == "" {
		return nil
	}
	var envs promotionv1alpha1.EnvironmentList
	if err := r.List(ctx, &envs, client.InNamespace(obj.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list Environments for Plugin", "plugin", obj.GetName())
		return nil
	}
	var reqs []reconcile.Request
	for i := range envs.Items {
		e := &envs.Items[i]
		if e.Spec.SourceRef.Name == pdName {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(e)})
		}
	}
	return reqs
}
