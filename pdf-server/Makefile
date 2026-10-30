SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := test

.PHONY: test validate
test:
	bash -n publish-corpus.sh
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v

validate: test validate-chart

SERVICE_RELEASE := pdf-server
include ../scripts/service-chart.mk
