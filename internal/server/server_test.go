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

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
)

var _ = Describe("Promotion server", func() {
	const (
		namespace = "demo"
		name      = "podinfo-6-9-0"
		version   = "6.9.0"
		option    = "replicaCount"
	)

	newRevision := func() *promotionv1alpha1.Revision {
		return &promotionv1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: promotionv1alpha1.RevisionSpec{
				Ref:    version,
				Digest: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				Definition: promotionv1alpha1.RevisionDefinition{
					Kind: greenhousev1alpha1.PluginDefinitionKind,
					Spec: promotionv1alpha1.RevisionDefinitionSpec{
						Version:     version,
						DisplayName: "Podinfo",
						Description: "Tiny web application",
						Icon:        "Widgets",
						HelmChart: &promotionv1alpha1.HelmChartReference{
							Name:       "podinfo",
							Repository: "oci://ghcr.io/stefanprodan/charts",
							Version:    version,
						},
						Options: []promotionv1alpha1.RevisionOption{
							{
								Name:        option,
								Default:     &apiextensionsv1.JSON{Raw: []byte("1")},
								Description: "Number of replicas",
								Type:        promotionv1alpha1.RevisionOptionTypeInt,
							},
							{Name: "ui.message", Required: true, Type: promotionv1alpha1.RevisionOptionTypeString},
						},
					},
				},
			},
		}
	}

	newClientBuilder := func() *fake.ClientBuilder {
		scheme := runtime.NewScheme()
		Expect(promotionv1alpha1.AddToScheme(scheme)).To(Succeed())
		return fake.NewClientBuilder().WithScheme(scheme)
	}

	resolve := func(reader client.Reader) *httptest.ResponseRecorder {
		path := "/apis/promotion.cloudoperators.dev/v1alpha1/namespaces/" + namespace + "/revisions/" + name + "/resolve"
		recorder := httptest.NewRecorder()
		NewHandler(reader).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		return recorder
	}

	resolvePluginDefinition := func(revision *promotionv1alpha1.Revision) *greenhousev1alpha1.PluginDefinition {
		recorder := resolve(newClientBuilder().WithObjects(revision).Build())
		Expect(recorder).To(HaveHTTPStatus(http.StatusOK))
		Expect(recorder).To(HaveHTTPHeaderWithValue("Content-Type", "application/json"))
		pluginDefinition := &greenhousev1alpha1.PluginDefinition{}
		Expect(json.NewDecoder(recorder.Body).Decode(pluginDefinition)).To(Succeed())
		return pluginDefinition
	}

	It("serves the Revision as a PluginDefinition", func() {
		pluginDefinition := resolvePluginDefinition(newRevision())

		Expect(pluginDefinition.Name).To(Equal(name))
		Expect(pluginDefinition.Namespace).To(Equal(namespace))
		Expect(pluginDefinition.Spec).To(Equal(greenhousev1alpha1.PluginDefinitionSpec{
			Version:     version,
			DisplayName: "Podinfo",
			Description: "Tiny web application",
			Icon:        "Widgets",
			HelmChart: &greenhousev1alpha1.HelmChartReference{
				Name:       "podinfo",
				Repository: "oci://ghcr.io/stefanprodan/charts",
				Version:    version,
			},
			Options: []greenhousev1alpha1.PluginOption{
				{
					Name:        option,
					Default:     &apiextensionsv1.JSON{Raw: []byte("1")},
					Description: "Number of replicas",
					Type:        greenhousev1alpha1.PluginOptionTypeInt,
				},
				{Name: "ui.message", Required: true, Type: greenhousev1alpha1.PluginOptionTypeString},
			},
		}))
	})

	It("answers 404 when the Revision does not exist", func() {
		recorder := resolve(newClientBuilder().Build())

		Expect(recorder).To(HaveHTTPStatus(http.StatusNotFound))
		Expect(recorder).To(HaveHTTPBody(ContainSubstring(`"` + name + `" not found`)))
	})

})
