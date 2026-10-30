SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := test

.PHONY: test validate
test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v

validate: test validate-chart
	bash -n helm-post-renderer.sh

SERVICE_RELEASE := nv-ingest
SERVICE_RENDER_ARGS = --post-renderer "$(SERVICE_ROOT)/helm-post-renderer.sh"
include ../scripts/service-chart.mk
