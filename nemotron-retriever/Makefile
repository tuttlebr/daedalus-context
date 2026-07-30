SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c

RELEASE ?= nemotron-retriever
NAMESPACE ?= daedalus-retriever
TIMEOUT ?= 30m

.PHONY: test render validate deploy status wait-ready smoke

test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v

render:
	helm template "$(RELEASE)" . --namespace "$(NAMESPACE)"

validate: test
	helm lint . --namespace "$(NAMESPACE)" --strict
	helm template "$(RELEASE)" . --namespace validation-nondefault >/dev/null

deploy:
	helm upgrade --install "$(RELEASE)" . --namespace "$(NAMESPACE)" \
	  --create-namespace --wait --timeout "$(TIMEOUT)"

status:
	helm status "$(RELEASE)" --namespace "$(NAMESPACE)"

wait-ready:
	kubectl --namespace="$(NAMESPACE)" rollout status deployment/nemotron-retriever-embedder --timeout="$(TIMEOUT)"
	kubectl --namespace="$(NAMESPACE)" rollout status deployment/nemotron-retriever-reranker --timeout="$(TIMEOUT)"

smoke:
	kubectl get --raw "/api/v1/namespaces/$(NAMESPACE)/services/http:nemotron-retriever-embedder:8000/proxy/adapter-health"
	kubectl get --raw "/api/v1/namespaces/$(NAMESPACE)/services/http:nemotron-retriever-reranker:8000/proxy/adapter-health"
