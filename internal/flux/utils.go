// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package flux

import (
	"os"
	"strings"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

const localRegistryEnv = "LOCAL_REGISTRY"

// ChartURLToName derives a valid Kubernetes resource name from a Helm repository URL.
// It strips common URL prefixes and replaces characters that are invalid in resource names.
func ChartURLToName(repositoryURL string) string {
	name := repositoryURL
	for _, prefix := range []string{"oci://", "https://", "http://"} {
		if after, ok := strings.CutPrefix(repositoryURL, prefix); ok {
			name = after
			break
		}
	}
	name = strings.TrimSuffix(name, "/")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

// GetSourceRepositoryType returns the Flux HelmRepository type for a given URL.
// OCI URLs use the OCI type; everything else uses the default (HTTP/HTTPS) type.
func GetSourceRepositoryType(repositoryURL string) string {
	if strings.HasPrefix(repositoryURL, "oci://") {
		return sourcev1.HelmRepositoryTypeOCI
	}
	return sourcev1.HelmRepositoryTypeDefault
}

// CheckIfLocalRegistry reports whether the given URL points at the local registry
// configured via the LOCAL_REGISTRY environment variable.
func CheckIfLocalRegistry(url string) bool {
	reg, ok := os.LookupEnv(localRegistryEnv)
	if !ok {
		return false
	}
	return strings.Contains(url, reg)
}
