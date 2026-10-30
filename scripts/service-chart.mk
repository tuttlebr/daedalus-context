# Shared local chart inspection. Runtime operations belong to the root Makefile.
HELM ?= helm
CHART_NAMESPACE ?= daedalus
SERVICE_ROOT := $(abspath $(dir $(firstword $(MAKEFILE_LIST))))
SERVICE_CHART := $(SERVICE_ROOT)/chart

.PHONY: render validate-chart
render:
	$(HELM) template "$(SERVICE_RELEASE)" "$(SERVICE_CHART)" \
	  --namespace "$(CHART_NAMESPACE)" $(SERVICE_RENDER_ARGS)

validate-chart:
	$(HELM) lint "$(SERVICE_CHART)" --namespace "$(CHART_NAMESPACE)" --strict
	@$(MAKE) --no-print-directory render >/dev/null
