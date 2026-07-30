SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c

RELEASE_NAME ?= nv-ingest
NAMESPACE ?= nv-ingest

.PHONY: test render validate deploy status wait-ready smoke

test:
	bash tests/test-deploy.sh

render:
	DRY_RUN=true bash deploy.sh

validate: test
	bash -n deploy.sh validate.sh tests/test-deploy.sh

deploy:
	bash deploy.sh

status:
	helm status "$(RELEASE_NAME)" --namespace "$(NAMESPACE)"

wait-ready:
	kubectl --namespace="$(NAMESPACE)" rollout status \
	  "deployment/$(RELEASE_NAME)" --timeout=20m

smoke:
	kubectl get --raw "/api/v1/namespaces/$(NAMESPACE)/services/http:$(RELEASE_NAME):7670/proxy/v1/health/ready"
