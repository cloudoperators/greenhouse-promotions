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

// Phase holds the per-reconcile state shared across the Source subroutines.
type Phase struct {
	Client client.Client
	Scheme *runtime.Scheme

	client     source.Client
	discovered []source.Version
}

// EnsureClient - constructs the source client (git or oci) from the Source spec and secret.
func (p *Phase) EnsureClient(src *promotionv1alpha1.Source) lifecycle.SubRoutine {
	return func(ctx context.Context) (lifecycle.Result, error) {
		c, err := p.buildClient(ctx, src)
		if err != nil {
			src.SetCondition(metav1.Condition{
				Type:    string(kmeta.ReadyCondition),
				Status:  metav1.ConditionFalse,
				Reason:  "SourceResolveFailed",
				Message: err.Error(),
			})
			return lifecycle.Break(), err
		}
		p.client = c
		return lifecycle.Continue(), nil
	}
}

// EnsureDiscovered - resolves versions from the source, keeps the newest DiscoveryLimit, newest first.
func (p *Phase) EnsureDiscovered(src *promotionv1alpha1.Source) lifecycle.SubRoutine {
	return func(ctx context.Context) (lifecycle.Result, error) {
		now := metav1.Now()
		src.Status.LastAttemptTime = &now

		versions, err := p.client.Discover(ctx)
		if err != nil {
			src.SetCondition(metav1.Condition{
				Type:    string(kmeta.ReadyCondition),
				Status:  metav1.ConditionFalse,
				Reason:  "DiscoveryFailed",
				Message: err.Error(),
			})
			return lifecycle.Break(), err
		}

		if limit := int(src.Spec.DiscoveryLimit); limit > 0 && len(versions) > limit {
			versions = versions[:limit]
		}
		p.discovered = versions

		newest := versions[0].Version
		if src.Status.LatestRevisionName != RevisionName(src.Name, newest) {
			src.Status.LastChangeTime = &now
		}
		return lifecycle.Continue(), nil
	}
}

// EnsureRevisions - creates a Revision for every discovered version not already present, fetching and freezing each definition at its ref.
func (p *Phase) EnsureRevisions(src *promotionv1alpha1.Source) lifecycle.SubRoutine {
	return func(ctx context.Context) (lifecycle.Result, error) {
		var filePath string
		if git := src.Spec.Source.Git; git != nil {
			filePath = git.Path
		}

		for _, v := range p.discovered {
			name := RevisionName(src.Name, v.Version)
			existing := &promotionv1alpha1.Revision{}
			err := p.Client.Get(ctx, types.NamespacedName{Namespace: src.Namespace, Name: name}, existing)
			if err == nil {
				continue // already frozen
			}
			if !apierrors.IsNotFound(err) {
				return lifecycle.Break(), err
			}

			raw, err := p.client.Fetch(ctx, v.Version, filePath)
			if err != nil {
				return lifecycle.Break(), err
			}
			defSpec, err := parseDefinition(raw)
			if err != nil {
				return lifecycle.Break(), err
			}
			applyOverrides(defSpec, src.Spec.Override)

			rev := &promotionv1alpha1.Revision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: src.Namespace,
					Labels: map[string]string{
						promotionv1alpha1.RevisionLabelSource: src.Name,
					},
				},
				Spec: promotionv1alpha1.RevisionSpec{
					Ref:        v.Version,
					Digest:     digestOf(raw),
					Definition: promotionv1alpha1.RevisionDefinition{Kind: "PluginDefinition", Spec: *defSpec},
				},
			}
			if err := controllerutil.SetControllerReference(src, rev, p.Scheme); err != nil {
				return lifecycle.Break(), err
			}
			if err := p.Client.Create(ctx, rev); err != nil && !apierrors.IsAlreadyExists(err) {
				return lifecycle.Break(), err
			}
		}
		return lifecycle.Continue(), nil
	}
}

// EnsureStatus - rebuilds revisions[] (ascending) mirroring each Revision's Ready state, sets latestRevisionName, and echoes the refresh annotation.
func (p *Phase) EnsureStatus(src *promotionv1alpha1.Source) lifecycle.SubRoutine {
	return func(ctx context.Context) (lifecycle.Result, error) {
		// p.discovered is newest-first; status.revisions is ascending (oldest-first).
		revs := make([]promotionv1alpha1.SourceRevision, 0, len(p.discovered))
		now := metav1.Now()
		for _, v := range slices.Backward(p.discovered) {
			name := RevisionName(src.Name, v.Version)

			ready := false
			createdAt := now
			rev := &promotionv1alpha1.Revision{}
			if err := p.Client.Get(ctx, types.NamespacedName{Namespace: src.Namespace, Name: name}, rev); err == nil {
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
		src.Status.Revisions = revs
		src.Status.LatestRevisionName = RevisionName(src.Name, p.discovered[0].Version)

		if v, ok := src.Annotations[promotionv1alpha1.AnnotationRefresh]; ok {
			src.Status.LastHandledRefresh = v
		}
		return lifecycle.Continue(), nil
	}
}

// buildClient - returns a git or oci source client for the Source, resolving auth from its secret.
func (p *Phase) buildClient(ctx context.Context, src *promotionv1alpha1.Source) (source.Client, error) {
	spec := src.Spec
	if spec.Source == nil {
		return nil, fmt.Errorf("source %s/%s: no source set", src.Namespace, src.Name)
	}
	switch {
	case spec.Source.Git != nil:
		git := spec.Source.Git
		auth, err := p.resolveAuth(ctx, src.Namespace, secretRefName(git.SecretRef))
		if err != nil {
			return nil, err
		}
		return source.NewGitClient(source.GitConfig{
			Repository:       git.URL,
			SemverConstraint: git.SemverConstraint,
			TagPattern:       git.TagPattern,
		}, auth)
	case spec.Source.Oci != nil:
		oci := spec.Source.Oci
		auth, err := p.resolveAuth(ctx, src.Namespace, ociSecretRefName(oci.SecretRef))
		if err != nil {
			return nil, err
		}
		cfg := source.OCIConfig{
			Repository:       oci.URL,
			SemverConstraint: oci.SemverConstraint,
		}
		if oci.LayerSelector != nil {
			cfg.LayerMediaType = oci.LayerSelector.MediaType
		}
		return source.NewOCIClient(cfg, auth)
	default:
		return nil, fmt.Errorf("source %s/%s: exactly one of git or oci must be set", src.Namespace, src.Name)
	}
}

// resolveAuth - loads credentials from the named secret in namespace, or returns empty auth when unset.
func (p *Phase) resolveAuth(ctx context.Context, namespace, secretName string) (source.Auth, error) {
	if secretName == "" {
		return source.Auth{}, nil
	}
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: secretName}
	if err := p.Client.Get(ctx, key, secret); err != nil {
		return source.Auth{}, fmt.Errorf("getting secret %s: %w", key, err)
	}
	return source.AuthFromData(secret.Data), nil
}

// RevisionName - derives a stable Revision name from the Source name and version, stripping any tag prefix.
func RevisionName(sourceName, version string) string {
	if i := strings.LastIndex(version, "/"); i >= 0 {
		version = version[i+1:]
	}
	v := strings.NewReplacer(".", "-", "+", "-").Replace(version)
	return fmt.Sprintf("%s-%s", sourceName, v)
}

// digestOf - returns the sha256 digest string of the given bytes.
func digestOf(b []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

// parseDefinition - unmarshals a PluginDefinition YAML into its spec, erroring when the version is empty.
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

// applyOverrides - mutates the definition in place, replacing option defaults and the chart repository per the Source overrides.
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

// secretRefName - returns the secret name from a git SecretKeyReference, or empty when nil.
func secretRefName(ref *promotionv1alpha1.SecretKeyReference) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}

// ociSecretRefName - returns the secret name from an oci SecretRef, or empty when nil.
func ociSecretRefName(ref *promotionv1alpha1.SecretRef) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}
