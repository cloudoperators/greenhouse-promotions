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
	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
)

var _ = Describe("Rollout Controller", func() {
	const (
		namespace        = "demo"
		revision         = "podinfo-6-9-0"
		cleanupFinalizer = "promotion.cloudoperators.dev/cleanup"
	)

	var (
		fakeClient client.Client
		reconciler *RolloutReconciler
	)

	newPlugin := func(name, stampedRevision string, ready bool) *greenhousev1alpha1.Plugin {
		plugin := &greenhousev1alpha1.Plugin{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: greenhousev1alpha1.PluginSpec{
				PluginDefinitionRef: greenhousev1alpha1.PluginDefinitionReference{Name: "podinfo", Kind: "PluginDefinition"},
			},
		}
		if stampedRevision != "" {
			plugin.Annotations = map[string]string{promotionv1alpha1.RevisionAnnotation: stampedRevision}
		}
		if ready {
			plugin.Status.SetConditions(greenhousemetav1alpha1.TrueCondition(greenhousemetav1alpha1.ReadyCondition, "", ""))
		} else {
			plugin.Status.SetConditions(greenhousemetav1alpha1.FalseCondition(greenhousemetav1alpha1.ReadyCondition, "", ""))
		}
		return plugin
	}

	newRollout := func(name string, pluginNames ...string) *promotionv1alpha1.Rollout {
		rollout := &promotionv1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Finalizers: []string{cleanupFinalizer}},
			Spec: promotionv1alpha1.RolloutSpec{
				RevisionRef: promotionv1alpha1.RevisionReference{Name: revision},
			},
		}
		for _, pluginName := range pluginNames {
			rollout.Spec.PluginRefs = append(rollout.Spec.PluginRefs, promotionv1alpha1.PluginReference{Name: pluginName})
		}
		return rollout
	}

	reconcileRollout := func(name string) *promotionv1alpha1.Rollout {
		key := types.NamespacedName{Name: name, Namespace: namespace}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		rollout := &promotionv1alpha1.Rollout{}
		Expect(fakeClient.Get(ctx, key, rollout)).To(Succeed())
		return rollout
	}

	stampedRevision := func(pluginName string) string {
		plugin := &greenhousev1alpha1.Plugin{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, plugin)).To(Succeed())
		return plugin.Annotations[promotionv1alpha1.RevisionAnnotation]
	}

	setup := func(objects ...client.Object) {
		scheme := runtime.NewScheme()
		Expect(promotionv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(greenhousev1alpha1.AddToScheme(scheme)).To(Succeed())
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&promotionv1alpha1.Rollout{}).
			WithObjects(objects...).
			Build()
		reconciler = &RolloutReconciler{Client: fakeClient, Scheme: scheme}
	}

	It("adds the cleanup finalizer before it touches the Plugins", func() {
		rollout := newRollout("promo", "podinfo-a")
		rollout.Finalizers = nil
		setup(newPlugin("podinfo-a", "", true), rollout)

		rollout = reconcileRollout("promo")

		Expect(rollout.Finalizers).To(ConsistOf(cleanupFinalizer))
		Expect(stampedRevision("podinfo-a")).To(BeEmpty())
	})

	It("stamps the revision on every referenced Plugin and reports Running", func() {
		setup(
			newPlugin("podinfo-a", "", true),
			newPlugin("podinfo-b", "podinfo-6-8-0", true),
			newRollout("promo", "podinfo-a", "podinfo-b"),
		)

		rollout := reconcileRollout("promo")

		Expect(stampedRevision("podinfo-a")).To(Equal(revision))
		Expect(stampedRevision("podinfo-b")).To(Equal(revision))
		Expect(rollout.Status.Phase).To(Equal(promotionv1alpha1.RolloutPhaseRunning))
		Expect(rollout.Status.StartedAt).NotTo(BeNil())
		Expect(rollout.Status.FinishedAt).To(BeNil())
		Expect(rollout.Status.Actor).To(Equal("rollout-controller"))
		Expect(rollout.Status.Targets).To(Equal(&promotionv1alpha1.RolloutTargets{Selected: 2, Converged: 0}))
	})

	It("stays Running while a stamped Plugin is not ready", func() {
		setup(
			newPlugin("podinfo-a", revision, true),
			newPlugin("podinfo-b", revision, false),
			newRollout("promo", "podinfo-a", "podinfo-b"),
		)

		rollout := reconcileRollout("promo")

		Expect(rollout.Status.Phase).To(Equal(promotionv1alpha1.RolloutPhaseRunning))
		Expect(rollout.Status.FinishedAt).To(BeNil())
		Expect(rollout.Status.Targets).To(Equal(&promotionv1alpha1.RolloutTargets{Selected: 2, Converged: 1}))
		Expect(meta.IsStatusConditionFalse(rollout.Status.Conditions, promotionv1alpha1.RolloutReadyCondition)).To(BeTrue())
	})

	It("succeeds once every stamped Plugin is ready", func() {
		setup(
			newPlugin("podinfo-a", revision, true),
			newPlugin("podinfo-b", revision, true),
			newRollout("promo", "podinfo-a", "podinfo-b"),
		)

		rollout := reconcileRollout("promo")

		Expect(rollout.Status.Phase).To(Equal(promotionv1alpha1.RolloutPhaseSucceeded))
		Expect(rollout.Status.FinishedAt).NotTo(BeNil())
		Expect(rollout.Status.Targets).To(Equal(&promotionv1alpha1.RolloutTargets{Selected: 2, Converged: 2}))
		Expect(meta.IsStatusConditionTrue(rollout.Status.Conditions, promotionv1alpha1.RolloutReadyCondition)).To(BeTrue())
	})

	It("fails when a referenced Plugin does not exist", func() {
		setup(
			newPlugin("podinfo-a", "", true),
			newRollout("promo", "podinfo-a", "podinfo-gone"),
		)

		rollout := reconcileRollout("promo")

		Expect(rollout.Status.Phase).To(Equal(promotionv1alpha1.RolloutPhaseFailed))
		Expect(rollout.Status.FinishedAt).NotTo(BeNil())
		ready := meta.FindStatusCondition(rollout.Status.Conditions, promotionv1alpha1.RolloutReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Message).To(ContainSubstring("podinfo-gone"))
		Expect(stampedRevision("podinfo-a")).To(BeEmpty())
	})

	It("leaves the Plugins alone once it has finished", func() {
		finished := newRollout("promo", "podinfo-a")
		finished.Status.Phase = promotionv1alpha1.RolloutPhaseSucceeded
		setup(newPlugin("podinfo-a", "podinfo-7-0-0", true), finished)

		rollout := reconcileRollout("promo")

		Expect(stampedRevision("podinfo-a")).To(Equal("podinfo-7-0-0"))
		Expect(rollout.Status.Phase).To(Equal(promotionv1alpha1.RolloutPhaseSucceeded))
	})

	It("lets a deleted Rollout go", func() {
		rollout := newRollout("promo", "podinfo-a")
		setup(newPlugin("podinfo-a", revision, true), rollout)
		Expect(fakeClient.Delete(ctx, rollout)).To(Succeed())
		key := client.ObjectKeyFromObject(rollout)

		for range 2 {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(apierrors.IsNotFound(fakeClient.Get(ctx, key, &promotionv1alpha1.Rollout{}))).To(BeTrue())
	})

	It("enqueues the unfinished Rollouts that reference a changed Plugin", func() {
		finished := newRollout("finished", "podinfo-a")
		finished.Status.Phase = promotionv1alpha1.RolloutPhaseFailed
		setup(
			newRollout("running", "podinfo-a", "podinfo-b"),
			newRollout("other", "podinfo-b"),
			finished,
		)

		requests := reconciler.rolloutsForPlugin(ctx, newPlugin("podinfo-a", "", true))

		Expect(requests).To(ConsistOf(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "running", Namespace: namespace},
		}))
	})
})
