SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := validate

.PHONY: test validate
test validate: validate-chart

SERVICE_RELEASE := milvus
SERVICE_RENDER_ARGS = --post-renderer "$(SERVICE_CHART)/post-render.py"
include ../scripts/service-chart.mk
