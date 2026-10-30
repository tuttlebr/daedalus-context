SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := test

.PHONY: test validate
test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v

validate: test validate-chart

SERVICE_RELEASE := nemotron-retriever
SERVICE_RENDER_ARGS = --values "$(SERVICE_CHART)/values-daedalus.yaml"
include ../scripts/service-chart.mk
