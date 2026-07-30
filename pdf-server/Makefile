SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c

NAMESPACE ?= milvus

.PHONY: test render validate deploy status wait-ready smoke publish

test:
	bash -n deploy.sh
	python3 -c 'import yaml; list(yaml.safe_load_all(open("k8s/pdf-server.yaml"))); list(yaml.safe_load_all(open("k8s/publish-corpus-job.yaml")))'
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v

render:
	TARGET=kubernetes DRY_RUN=true NAMESPACE="$(NAMESPACE)" bash deploy.sh

validate: test render

deploy:
	TARGET=kubernetes NAMESPACE="$(NAMESPACE)" bash deploy.sh

status:
	kubectl --namespace="$(NAMESPACE)" get \
	  deployment/pdf-server service/pdf-server \
	  poddisruptionbudget/pdf-server persistentvolumeclaim/pdf-server-data

wait-ready:
	kubectl --namespace="$(NAMESPACE)" rollout status deployment/pdf-server --timeout=10m

smoke:
	kubectl get --raw "/api/v1/namespaces/$(NAMESPACE)/services/http:pdf-server:80/proxy/readyz"

publish:
	TARGET=kubernetes PUBLISH_CORPUS=true NAMESPACE="$(NAMESPACE)" bash deploy.sh
