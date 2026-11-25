// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// Package source holds the reconcile phases for the Source controller.
package source

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	kmeta "github.com/cloudoperators/greenhouse-promotions/api/meta"
	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/lifecycle"
	"github.com/cloudoperators/greenhouse-promotions/internal/source"
)

// Phase holds the per-reconcile state shared across a Source's subroutine chain.
// The controller constructs it as &source.Phase{...} and assembles its exported
// methods into the routine slice; each method reads what earlier phases set and
// writes what later phases need.
type Phase struct {
	Client client.Client
	Scheme *runtime.Scheme
	Source *promotionv1alpha1.Source

	client     source.Client
	discovered []source.Version
}

// ResolveClient builds the source.Client (git or oci) from spec, loading auth
// from the referenced Secret when present.
func (p *Phase) ResolveClient(ctx context.Context) (lifecycle.Result, error) {
	spec := p.Source.Spec
	if spec.Source == nil {
		return lifecycle.Break(), fmt.Errorf("source: spec.source is empty")
	}

	switch {
	case spec.Source.Git != nil:
		git := spec.Source.Git
		auth, err := p.authFor(ctx, secretRefName(git.SecretRef))
		if err != nil {
			return lifecycle.Break(), err
		}
		c, err := source.NewGitClient(source.GitConfig{
			Repository:       git.URL,
			SemverConstraint: git.SemverConstraint,
			TagPattern:       git.TagPattern,
		}, auth)
		if err != nil {
			return lifecycle.Break(), err
		}
		p.client = c
	case spec.Source.Oci != nil:
		oci := spec.Source.Oci
		auth, err := p.authFor(ctx, ociSecretRefName(oci.SecretRef))
		if err != nil {
			return lifecycle.Break(), err
		}
		c, err := source.NewOCIClient(source.OCIConfig{
			Repository:       oci.URL,
			SemverConstraint: oci.SemverConstraint,
		}, auth)
		if err != nil {
			return lifecycle.Break(), err
		}
		p.client = c
	default:
		return lifecycle.Break(), fmt.Errorf("source: exactly one of git or oci must be set")
	}
	return lifecycle.Continue(), nil
}

// Discover lists versions newest-first and stamps attempt/change timestamps.
func (p *Phase) Discover(ctx context.Context) (lifecycle.Result, error) {
	now := metav1.Now()
	p.Source.Status.LastAttemptTime = &now

	versions, err := p.client.Discover(ctx)
	if err != nil {
		p.Source.SetCondition(metav1.Condition{
			Type:    string(kmeta.ReadyCondition),
			Status:  metav1.ConditionFalse,
			Reason:  "DiscoveryFailed",
			Message: err.Error(),
		})
		return lifecycle.Break(), err
	}

	// Cap to the newest DiscoveryLimit versions (client returns newest-first).
	if limit := int(p.Source.Spec.DiscoveryLimit); limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	p.discovered = versions

	newest := versions[0].Version
	if p.Source.Status.LatestRevisionName != RevisionName(p.Source.Name, newest) {
		p.Source.Status.LastChangeTime = &now
	}
	return lifecycle.Continue(), nil
}

// CreateRevisions fetches the definition file at the newest ref and creates the
// CreateRevisions creates a Revision for every discovered version that does not
// already exist, fetching and freezing each definition at its ref.
func (p *Phase) CreateRevisions(ctx context.Context) (lifecycle.Result, error) {
	git := p.Source.Spec.Source.Git
	if git == nil {
		// oci freeze not implemented yet; discovery status still reconciles.
		return lifecycle.Continue(), nil
	}

	for _, v := range p.discovered {
		name := RevisionName(p.Source.Name, v.Version)
		existing := &promotionv1alpha1.Revision{}
		err := p.Client.Get(ctx, types.NamespacedName{Namespace: p.Source.Namespace, Name: name}, existing)
		if err == nil {
			continue // already frozen
		}
		if !apierrors.IsNotFound(err) {
			return lifecycle.Break(), err
		}

		raw, err := p.client.Fetch(ctx, v.Version, git.Path)
		if err != nil {
			return lifecycle.Break(), err
		}
		defSpec, err := parseDefinition(raw)
		if err != nil {
			return lifecycle.Break(), err
		}
		applyOverrides(defSpec, p.Source.Spec.Override)

		rev := &promotionv1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: p.Source.Namespace,
				Labels: map[string]string{
					promotionv1alpha1.RevisionLabelSource: p.Source.Name,
				},
			},
			Spec: promotionv1alpha1.RevisionSpec{
				Ref:        v.Version,
				Digest:     digestOf(raw),
				Definition: promotionv1alpha1.RevisionDefinition{Kind: "PluginDefinition", Spec: *defSpec},
			},
		}
		if err := controllerutil.SetControllerReference(p.Source, rev, p.Scheme); err != nil {
			return lifecycle.Break(), err
		}
		if err := p.Client.Create(ctx, rev); err != nil && !apierrors.IsAlreadyExists(err) {
			return lifecycle.Break(), err
		}
	}
	return lifecycle.Continue(), nil
}

// ReconcileStatus rebuilds revisions[] (ascending, oldest-first) with each
// Revision's Ready state mirrored in, sets latestRevisionName to the newest, and
// echoes the refresh annotation.
func (p *Phase) ReconcileStatus(ctx context.Context) (lifecycle.Result, error) {
	// p.discovered is newest-first; status.revisions is ascending (oldest-first).
	revs := make([]promotionv1alpha1.SourceRevision, 0, len(p.discovered))
	now := metav1.Now()
	for _, v := range slices.Backward(p.discovered) {
		name := RevisionName(p.Source.Name, v.Version)

		ready := false
		createdAt := now
		rev := &promotionv1alpha1.Revision{}
		if err := p.Client.Get(ctx, types.NamespacedName{Namespace: p.Source.Namespace, Name: name}, rev); err == nil {
			if c := rev.GetReadyCondition(); c != nil && c.Status == metav1.ConditionTrue {
				ready = true
			}
			createdAt = rev.CreationTimestamp
		} else if !apierrors.IsNotFound(err) {
			return lifecycle.Break(), err
		}

		revs = append(revs, promotionv1alpha1.SourceRevision{
			Name:      name,
			Digest:    v.Revision,
			Ready:     ready,
			CreatedAt: createdAt,
		})
	}
	p.Source.Status.Revisions = revs

	newest := p.discovered[0].Version
	p.Source.Status.LatestRevisionName = RevisionName(p.Source.Name, newest)

	if v, ok := p.Source.Annotations[promotionv1alpha1.AnnotationRefresh]; ok {
		p.Source.Status.LastHandledRefresh = v
	}
	return lifecycle.Continue(), nil
}

func (p *Phase) authFor(ctx context.Context, secretName string) (source.Auth, error) {
	if secretName == "" {
		return source.Auth{}, nil
	}
	secret := &corev1.Secret{}
	if err := p.Client.Get(ctx, types.NamespacedName{Namespace: p.Source.Namespace, Name: secretName}, secret); err != nil {
		return source.Auth{}, fmt.Errorf("source: load auth secret %q: %w", secretName, err)
	}
	return source.AuthFromData(secret.Data), nil
}

// RevisionName derives a stable Revision name from the Source and version.
func RevisionName(sourceName, version string) string {
	if i := strings.LastIndex(version, "/"); i >= 0 {
		version = version[i+1:]
	}
	v := strings.NewReplacer(".", "-", "+", "-").Replace(version)
	return fmt.Sprintf("%s-%s", sourceName, v)
}

func digestOf(b []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

func parseDefinition(raw []byte) (*promotionv1alpha1.RevisionDefinitionSpec, error) {
	var doc struct {
		Spec promotionv1alpha1.RevisionDefinitionSpec `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("source: parse definition: %w", err)
	}
	if doc.Spec.Version == "" {
		return nil, fmt.Errorf("source: parsed definition has empty spec.version")
	}
	return &doc.Spec, nil
}

// applyOverrides mutates the resolved definition in place with the Source's
// overrides: each named option's default is replaced, and the helm chart
// repository is rewritten when set.
func applyOverrides(spec *promotionv1alpha1.RevisionDefinitionSpec, o promotionv1alpha1.Override) {
	if o.Repository != "" && spec.HelmChart != nil {
		spec.HelmChart.Repository = o.Repository
	}
	if len(o.OptionsOverride) == 0 {
		return
	}
	byName := make(map[string]*apiextensionsv1.JSON, len(o.OptionsOverride))
	for _, ov := range o.OptionsOverride {
		byName[ov.Name] = ov.Value
	}
	for i := range spec.Options {
		if val, ok := byName[spec.Options[i].Name]; ok {
			spec.Options[i].Default = val
		}
	}
}

func secretRefName(ref *promotionv1alpha1.SecretKeyReference) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}

func ociSecretRefName(ref *promotionv1alpha1.SecretRef) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}
