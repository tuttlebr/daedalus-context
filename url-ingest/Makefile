SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c


.PHONY: test validate docker-build docker-push

test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
	docker build --target check .

docker-build:
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  --profile ingest build url-ingest nv-ingest-ms-tasks

docker-push: docker-build
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  --profile ingest push url-ingest nv-ingest-ms-tasks

validate: test validate-chart

SERVICE_RELEASE := url-ingest
include ../scripts/service-chart.mk
