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

	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
)

const resolvePattern = "GET /apis/promotion.cloudoperators.dev/v1alpha1/namespaces/{namespace}/revisions/{name}/resolve"

func NewHandler(reader client.Reader) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(resolvePattern, func(w http.ResponseWriter, r *http.Request) {
		revision := &promotionv1alpha1.Revision{}
		key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
		if err := reader.Get(r.Context(), key, revision); err != nil {
			status := http.StatusInternalServerError
			if apierrors.IsNotFound(err) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}

		pluginDefinition, err := pluginDefinitionFor(revision)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pluginDefinition); err != nil {
			log.FromContext(r.Context()).Error(err, "Failed to write the PluginDefinition", "revision", key)
		}
	})
	return mux
}

func pluginDefinitionFor(revision *promotionv1alpha1.Revision) (*greenhousev1alpha1.PluginDefinition, error) {
	pluginDefinition := &greenhousev1alpha1.PluginDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: greenhousev1alpha1.GroupVersion.String(),
			Kind:       revision.Spec.Definition.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: revision.Name, Namespace: revision.Namespace},
	}

	spec, err := json.Marshal(revision.Spec.Definition.Spec)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(spec, &pluginDefinition.Spec); err != nil {
		return nil, err
	}

	for _, override := range revision.Spec.OptionOverrides {
		for i := range pluginDefinition.Spec.Options {
			if pluginDefinition.Spec.Options[i].Name == override.Name {
				pluginDefinition.Spec.Options[i].Default = override.Value
			}
		}
	}
	return pluginDefinition, nil
}
