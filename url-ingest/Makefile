SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c

NAMESPACE ?= milvus
COLLECTIONS ?=

.PHONY: test render validate docker-build docker-push deploy status submit observe verify-run

test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
	docker build --target check .

render:
	NAMESPACE="$(NAMESPACE)" bash k8s/deploy.sh --render-only

validate: test
	bash -n deploy.sh k8s/deploy.sh k8s/lib/job-control.sh
	python3 -m py_compile nv-ingest-ms-tasks.py k8s/render-manual-job.py

docker-build:
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  --profile ingest build url-ingest nv-ingest-ms-tasks

docker-push: docker-build
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  --profile ingest push url-ingest nv-ingest-ms-tasks

deploy:
	NAMESPACE="$(NAMESPACE)" bash k8s/deploy.sh

status:
	kubectl --namespace="$(NAMESPACE)" get \
	  cronjob/url-ingest-nvidia \
	  cronjob/url-ingest-kubernetes \
	  cronjob/url-ingest-vetpartner \
	  cronjob/url-ingest-semianalysis \
	  cronjob/url-ingest-mentalhealth

submit:
	NAMESPACE="$(NAMESPACE)" bash k8s/deploy.sh --run-now $(COLLECTIONS)

observe:
	kubectl --namespace="$(NAMESPACE)" get jobs,pods \
	  --selector=app.kubernetes.io/name=url-ingest

verify-run:
	@echo "verification is synchronous with submit; inspect persisted status and collection health" >&2
	@exit 64
