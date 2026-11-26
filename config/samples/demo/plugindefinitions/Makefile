# Push PluginDefinition YAMLs to an OCI registry as individual artifacts.
#
# Each <app>/*.yaml is pushed to:
#   $(REGISTRY)/$(REPO_PREFIX)/<app>:<.spec.version>
# with oras, using the version read from each file's .spec.version.

REGISTRY    ?= localhost:5000
REPO_PREFIX ?= plugindefinitions
MEDIA_TYPE  ?= application/vnd.greenhouse.plugindefinition.layer.v1+yaml

ORAS ?= oras
YQ   ?= yq

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# push-app: push every *.yaml in directory $(1) to <registry>/<prefix>/$(1):<version>
define push-app
	@set -eu; \
	for f in $(1)/*.yaml; do \
		[ -e "$$f" ] || { echo "no yaml files in $(1)"; break; }; \
		version=$$($(YQ) -r '.spec.version' "$$f"); \
		if [ -z "$$version" ] || [ "$$version" = "null" ]; then \
			echo "SKIP $$f: no .spec.version"; continue; \
		fi; \
		ref="$(REGISTRY)/$(REPO_PREFIX)/$(1):$$version"; \
		echo "push $$f -> $$ref"; \
		$(ORAS) push "$$ref" \
			--artifact-type application/vnd.greenhouse.plugindefinition \
			"$$f:$(MEDIA_TYPE)"; \
	done
endef

.PHONY: podinfo-oci
podinfo-oci: ## Push all podinfo PluginDefinitions to OCI
	$(call push-app,podinfo)

.PHONY: podinfo-push
podinfo-push: ## Push a single podinfo PluginDefinition to OCI (FILE=path/to.yaml)
	@test -n "$(FILE)" || { echo "usage: make podinfo-push FILE=<path>"; exit 1; }
	@test -e "$(FILE)" || { echo "no such file: $(FILE)"; exit 1; }
	@version=$$($(YQ) -r '.spec.version' "$(FILE)"); \
	if [ -z "$$version" ] || [ "$$version" = "null" ]; then \
		echo "SKIP $(FILE): no .spec.version"; exit 1; \
	fi; \
	ref="$(REGISTRY)/$(REPO_PREFIX)/podinfo:$$version"; \
	echo "push $(FILE) -> $$ref"; \
	$(ORAS) push "$$ref" \
		--artifact-type application/vnd.greenhouse.plugindefinition \
		"$(FILE):$(MEDIA_TYPE)"

.PHONY: perses-oci
perses-oci: ## Push all perses PluginDefinitions to OCI
	$(call push-app,perses)

.PHONY: cert-manager-oci
cert-manager-oci: ## Push all cert-manager PluginDefinitions to OCI
	$(call push-app,cert-manager)

.PHONY: oci
oci: podinfo-oci perses-oci cert-manager-oci ## Push all apps