SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c

RELEASE_NAME ?= milvus
NAMESPACE ?= milvus
CHART ?= zilliztech/milvus
CHART_VERSION ?= 5.0.14

.PHONY: test render validate deploy status wait-ready smoke backup restore-verify

test validate:
	bash validate.sh

render:
	helm template "$(RELEASE_NAME)" "$(CHART)" --version "$(CHART_VERSION)" \
	  --namespace "$(NAMESPACE)" -f custom-values.yaml
	@printf '%s\n' '---'
	@sed -e 's|__NAMESPACE__|$(NAMESPACE)|g' \
	  -e 's|__RELEASE_NAME__|$(RELEASE_NAME)|g' k8s/pdb.yaml

deploy:
	bash deploy.sh

status:
	helm status "$(RELEASE_NAME)" --namespace "$(NAMESPACE)"

wait-ready:
	@for component in proxy querynode datanode streamingnode mixcoord; do \
	  kubectl --namespace="$(NAMESPACE)" rollout status \
	    "deployment/$(RELEASE_NAME)-$$component" --timeout=20m; \
	done

smoke:
	kubectl get --raw "/api/v1/namespaces/$(NAMESPACE)/services/http:$(RELEASE_NAME):9091/proxy/healthz"

backup restore-verify:
	@echo "unsupported for stateful wrapper until an approved backup/restore profile is configured" >&2
	@exit 64
