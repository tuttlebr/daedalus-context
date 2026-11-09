SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c


.PHONY: test validate docker-build docker-push

test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
	docker build --target check .

docker-build:
	$(MAKE) -C .. images-build IMAGE_BUILD_SERVICES="url-ingest nv-ingest-ms-tasks"

docker-push:
	$(MAKE) -C .. images-push IMAGE_BUILD_SERVICES="url-ingest nv-ingest-ms-tasks"

validate: test validate-chart

SERVICE_RELEASE := url-ingest
include ../scripts/service-chart.mk
