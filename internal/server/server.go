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
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	promotionv1alpha1 "github.com/cloudoperators/greenhouse-promotions/api/promotion/v1alpha1"
	"github.com/cloudoperators/greenhouse-promotions/internal/flux"
)

const resolvePath = "/apis/promotion.cloudoperators.dev/v1alpha1/namespaces/:namespace/revisions/:name/resolve"

type resolvedPluginDefinition struct {
	*greenhousev1alpha1.PluginDefinition
	Status resolvedStatus `json:"status"`
}

type resolvedStatus struct {
	Flux *fluxResources `json:"flux,omitempty"`
}

type fluxResources struct {
	HelmChart      string `json:"helmChart"`
	HelmRepository string `json:"helmRepository"`
}

func NewHandler(reader client.Reader) http.Handler {
	e := echo.New()
	e.Use(middleware.Recover())
	e.GET(resolvePath, func(c echo.Context) error {
		revision := &promotionv1alpha1.Revision{}
		key := client.ObjectKey{Namespace: c.Param("namespace"), Name: c.Param("name")}
		if err := reader.Get(c.Request().Context(), key, revision); err != nil {
			if apierrors.IsNotFound(err) {
				return echo.NewHTTPError(http.StatusNotFound, err.Error())
			}
			return err
		}

		pluginDefinition, err := pluginDefinitionFor(revision)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, resolvedPluginDefinition{
			PluginDefinition: pluginDefinition,
			Status:           resolvedStatus{Flux: fluxResourcesFor(revision)},
		})
	})
	return e
}

func pluginDefinitionFor(revision *promotionv1alpha1.Revision) (*greenhousev1alpha1.PluginDefinition, error) {
	pluginDefinition := &greenhousev1alpha1.PluginDefinition{}
	pluginDefinition.SetName(revision.Name)
	pluginDefinition.SetNamespace(revision.Namespace)

	spec, err := json.Marshal(revision.Spec.Definition.Spec)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(spec, &pluginDefinition.Spec); err != nil {
		return nil, err
	}
	return pluginDefinition, nil
}

func fluxResourcesFor(revision *promotionv1alpha1.Revision) *fluxResources {
	helmChart := revision.Spec.Definition.Spec.HelmChart
	if helmChart == nil {
		return nil
	}
	return &fluxResources{
		HelmChart:      revision.FluxHelmChartResourceName(),
		HelmRepository: flux.ChartURLToName(helmChart.Repository),
	}
}
