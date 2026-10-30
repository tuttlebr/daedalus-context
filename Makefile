SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

# Always recurse with the GNU Make executable that is parsing this file. An
# exported MAKE value must not redirect doctor or component operations to an
# unrelated command.
override MAKE := $(MAKE_COMMAND)

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
comma := ,
KUBECTL ?= kubectl
HELM ?= helm
JQ ?= jq
COMPOSE ?= docker compose
ENV_FILE ?= $(ROOT_DIR)/.env
IMAGE_BUILD_SERVICES := k8s-mcp-server-read-only unifi-network-mcp-server pdf-server url-ingest nv-ingest-ms-tasks nv-ingest-parser
DAEDALUS_CONTEXT_IMAGE_VERSION ?= 0.1.0
EXPECTED_CONTEXT ?=
ROLLOUT_TIMEOUT ?= 20m
ENVIRONMENT ?= production
GENERATED_AGENT_ENV ?= $(ROOT_DIR)/stack/generated/$(ENVIRONMENT)-daedalus-context.env
COMPOSE_FILE := $(ROOT_DIR)/docker-compose.yaml

# Every context aid and the Agent share one reviewed namespace. Component
# aliases remain for the existing health/data scripts, but cannot diverge.
AGENT_NAMESPACE ?= daedalus
AGENT_WORKLOAD ?= deployment/daedalus-backend-default
DAEDALUS_NAMESPACE ?= daedalus
override MILVUS_NAMESPACE := $(DAEDALUS_NAMESPACE)
override NV_INGEST_NAMESPACE := $(DAEDALUS_NAMESPACE)
override PDF_SERVER_NAMESPACE := $(DAEDALUS_NAMESPACE)
override URL_INGEST_NAMESPACE := $(DAEDALUS_NAMESPACE)
override K8S_MCP_NAMESPACE := $(DAEDALUS_NAMESPACE)
override UNIFI_MCP_NAMESPACE := $(DAEDALUS_NAMESPACE)
override RETRIEVER_NAMESPACE := $(DAEDALUS_NAMESPACE)
K8S_MCP_NODE_PORT ?= 30800
K8S_MCP_READ_ONLY ?= true
K8S_MCP_NO_HELM ?= true
K8S_MCP_ENABLE_EXEC ?= false
K8S_MCP_ENABLE_KUBECTL ?= false
K8S_MCP_ENABLE_AGENT_WRITE ?= false
K8S_MCP_VALUES_FILE ?=
UNIFI_MCP_NODE_PORT ?= 30081
MCP_EXTERNAL_CLIENT_CIDR ?= 192.168.1.0/24
MCP_EXTERNAL_ENABLED ?= false
UNIFI_CONTROLLER_CA_FILE ?=
UNIFI_CONTROLLER_HOSTNAME ?=
UNIFI_CONTROLLER_IP ?=
URL_INGEST_SUSPEND ?= true

MILVUS_RELEASE ?= milvus
NV_INGEST_RELEASE ?= nv-ingest
RETRIEVER_RELEASE ?= nemotron-retriever
RETRIEVER_FULLNAME ?= nemotron-retriever
RETRIEVER_CHART ?= $(ROOT_DIR)/nemotron-retriever/chart
MILVUS_CHART ?= $(ROOT_DIR)/nim-milvus-ms/chart
NV_INGEST_CHART ?= $(ROOT_DIR)/nv-ingest/chart
PDF_SERVER_CHART ?= $(ROOT_DIR)/pdf-server/chart
URL_INGEST_CHART ?= $(ROOT_DIR)/url-ingest/chart
K8S_MCP_CHART ?= $(ROOT_DIR)/k8s-multicluster-mcp/chart
UNIFI_MCP_CHART ?= $(ROOT_DIR)/unifi-network-mcp/chart
HINDSIGHT_ROOT ?= $(abspath $(ROOT_DIR)/../daedalus-hindsight)
HINDSIGHT_CHART ?= $(HINDSIGHT_ROOT)/charts/daedalus-hindsight
HINDSIGHT_RELEASE ?= hindsight
# Production-safe chart defaults are used unless the operator explicitly names
# an override. NodePort examples are never auto-discovered.
RETRIEVER_VALUES_FILE ?= nemotron-retriever/chart/values-daedalus.yaml
RETRIEVER_ROLLOUT_TIMEOUT ?= 30m
MILVUS_SERVICE ?= $(MILVUS_RELEASE)
MINIO_SERVICE ?= $(MILVUS_RELEASE)-minio
NV_INGEST_SERVICE ?= $(NV_INGEST_RELEASE)
MILVUS_ROOT_SECRET_NAME ?= milvus-root-credentials
# MinIO bulk data must not silently inherit the etcd-local class. Deployments
# must select a reviewed storage class explicitly.
MINIO_STORAGE_CLASS ?=
ETCD_STORAGE_CLASS ?= local-etcd
NV_INGEST_VERSION ?= 26.3.0

RETRIEVER_EMBEDDING_SERVICE ?= $(RETRIEVER_FULLNAME)-embedder
RETRIEVER_RERANKER_SERVICE ?= $(RETRIEVER_FULLNAME)-reranker
override EMBEDDING_NIM_ENDPOINT := http://$(RETRIEVER_EMBEDDING_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local:8000/v1
EMBEDDING_NIM_MODEL_NAME ?= nvidia/Nemotron-3.5-Embed-1B-NVFP4
override DAEDALUS_RERANKER_ENDPOINT := http://$(RETRIEVER_RERANKER_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local:8000/rerank
override NIM_RERANKER_ENDPOINT := http://$(RETRIEVER_RERANKER_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local:8000/v1/ranking
RERANKER_MODEL_NAME ?= nvidia/Nemotron-3.5-Rerank-8B-BF16

override MILVUS_ENDPOINT := http://$(MILVUS_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local:19530
override MINIO_ENDPOINT := http://$(MINIO_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local:9000
override NV_INGEST_ENDPOINT := http://$(NV_INGEST_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local:7670
override HINDSIGHT_API_URL := http://hindsight-api.$(DAEDALUS_NAMESPACE).svc.cluster.local:8888
override NV_INGEST_MESSAGE_HOST := $(NV_INGEST_SERVICE).$(DAEDALUS_NAMESPACE).svc.cluster.local
NV_INGEST_MESSAGE_PORT ?= 7670
# Each replica reserves 24 CPU and 24 GiB. Two distinct nodes currently meet
# that placement contract; a third replica would remain Pending.
NV_INGEST_REPLICA_COUNT ?= 2
# Production operators select the shared URI as IMAGE_STORAGE_URI in .env.
# The prefixed alias is available for an explicit chart-only override.
NV_INGEST_IMAGE_STORAGE_URI ?=
EFFECTIVE_NV_INGEST_IMAGE_STORAGE_URI := $(or $(strip $(NV_INGEST_IMAGE_STORAGE_URI)),$(strip $(IMAGE_STORAGE_URI)))
OCR_HTTP_ENDPOINT ?=
OCR_INFER_PROTOCOL ?= http
OCR_MODEL_NAME ?= scene_text_ensemble
YOLOX_HTTP_ENDPOINT ?=
YOLOX_INFER_PROTOCOL ?= http
YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT ?=
YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL ?= http
YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT ?=
YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL ?= http
VLM_CAPTION_ENDPOINT ?=
VLM_CAPTION_MODEL_NAME ?= nvidia/NVIDIA-Nemotron-Nano-12B-v2
NEMOTRON_PARSE_HTTP_ENDPOINT ?=
NEMOTRON_PARSE_INFER_PROTOCOL ?= http
NEMOTRON_PARSE_MODEL_NAME ?= nvidia/nemotron-parse
NV_INGEST_PARSER_IMAGE ?= daedalus.ddns.me:5050/btuttle/nemotron-parse:0.1.3
LOCAL_DOCUMENT_PARSE_ENDPOINT ?= http://nv-ingest-parser.$(DAEDALUS_NAMESPACE).svc.cluster.local:8000
MINIO_BUCKET ?= nv-ingest
URL_INGEST_SOURCE_NODE ?= daedalus-01
URL_INGEST_GLOBAL_CAPACITY ?= 1
INGEST_JOB_DEADLINE_SECONDS ?= 21600
URL_INGEST_RUNNING_GRACE_SECONDS ?= 86400
REQUIRED_COLLECTIONS ?= nvidia kubernetes vetpartner semianalysis mentalhealth
PDF_SERVER_IMAGE ?= daedalus.ddns.me:5050/btuttle/pdf-server:$(DAEDALUS_CONTEXT_IMAGE_VERSION)
PDF_STORAGE_CLASS ?= nfs-client
PDF_STORAGE_SIZE ?= 10Gi
PDF_SOURCE_HOST_PATH ?= $(ROOT_DIR)/pdf-server
PDF_SOURCE_NODE ?= $(URL_INGEST_SOURCE_NODE)
CORPUS_GENERATION ?=

# Build and publish are deliberately separate from deployment. Both deploy and
# update reconcile an already-published SemVer image release.
EXPOSE_NODEPORTS ?= false
ENABLE_MILVUS_MONITORING ?= false
CHECK_CONSUMER ?= true
CONSUMER_RETRIES ?= 2

SUPPORTED_COMPONENTS := hindsight milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp
COMPONENTS ?= $(SUPPORTED_COMPONENTS)
RELEASE_hindsight := $(HINDSIGHT_RELEASE)
RELEASE_milvus := $(MILVUS_RELEASE)
RELEASE_retriever := $(RETRIEVER_RELEASE)
RELEASE_nv-ingest := $(NV_INGEST_RELEASE)
RELEASE_pdf-server := pdf-server
RELEASE_k8s-mcp := k8s-mcp-server
RELEASE_unifi-mcp := unifi-network-mcp-server
# URL ingestion has CronJobs, not long-running workloads to restart.
RECOVERY_RELEASE_ARGS = $(foreach component,$(filter-out url-ingest,$(COMPONENTS)),--release $(RELEASE_$(component)))

UNKNOWN_COMPONENTS := $(filter-out $(SUPPORTED_COMPONENTS),$(COMPONENTS))

ifneq ($(strip $(UNKNOWN_COMPONENTS)),)
$(error Unsupported COMPONENTS: $(UNKNOWN_COMPONENTS). Choose from: $(SUPPORTED_COMPONENTS))
endif

.PHONY: help doctor show-config require-context \
	images-validate images-build images-push images-release-refs images-publish \
	compose-config compose-build compose-push compose-release-refs compose-publish \
	preflight validate validate-helm validate-local validate-cluster validate-ha render-agent-env deploy deploy-workflow update redeploy restart recovery-status recovery-status-workflow \
	verify-shared-secrets migrate-legacy-secrets migrate-hindsight deploy-local-fast-storage deploy-nfs-locking patch-nfs-pv-locking enable-schedules enable-external-mcp cleanup-legacy \
	healthcheck health-provider health-dependencies health-integration health-stack \
	healthcheck-agent publish-corpus stage-data seed-data run-ingest \
	deploy-hindsight deploy-milvus deploy-retriever deploy-nv-ingest deploy-pdf-server deploy-url-ingest deploy-k8s-mcp deploy-unifi-mcp \
	update-hindsight update-milvus update-retriever update-nv-ingest update-pdf-server update-url-ingest update-k8s-mcp update-unifi-mcp \
	redeploy-hindsight redeploy-milvus redeploy-retriever redeploy-nv-ingest redeploy-pdf-server redeploy-url-ingest redeploy-k8s-mcp redeploy-unifi-mcp \
	health-provider-hindsight health-provider-milvus health-provider-retriever health-provider-nv-ingest health-provider-pdf-server \
	health-provider-url-ingest health-provider-k8s-mcp health-provider-unifi-mcp \
	health-dependencies-hindsight health-dependencies-milvus health-dependencies-retriever health-dependencies-nv-ingest \
	health-dependencies-pdf-server health-dependencies-url-ingest health-dependencies-k8s-mcp health-dependencies-unifi-mcp \
	health-integration-hindsight health-integration-milvus health-integration-retriever health-integration-nv-ingest health-integration-pdf-server \
	health-integration-url-ingest health-integration-k8s-mcp health-integration-unifi-mcp \
	healthcheck-hindsight healthcheck-milvus healthcheck-retriever healthcheck-nv-ingest healthcheck-pdf-server healthcheck-url-ingest \
	healthcheck-k8s-mcp healthcheck-unifi-mcp

CLUSTER_TARGETS := preflight validate validate-cluster deploy-workflow update redeploy recovery-status-workflow \
	verify-shared-secrets migrate-legacy-secrets migrate-hindsight deploy-local-fast-storage deploy-nfs-locking patch-nfs-pv-locking enable-schedules enable-external-mcp cleanup-legacy \
	health-provider health-dependencies health-integration health-stack healthcheck healthcheck-agent \
	publish-corpus stage-data seed-data run-ingest \
	deploy-hindsight deploy-milvus deploy-retriever deploy-nv-ingest deploy-pdf-server deploy-url-ingest deploy-k8s-mcp deploy-unifi-mcp \
	update-hindsight update-milvus update-retriever update-nv-ingest update-pdf-server update-url-ingest update-k8s-mcp update-unifi-mcp \
	redeploy-hindsight redeploy-milvus redeploy-retriever redeploy-nv-ingest redeploy-pdf-server redeploy-url-ingest redeploy-k8s-mcp redeploy-unifi-mcp \
	health-provider-hindsight health-provider-milvus health-provider-retriever health-provider-nv-ingest health-provider-pdf-server \
	health-provider-url-ingest health-provider-k8s-mcp health-provider-unifi-mcp \
	health-dependencies-hindsight health-dependencies-milvus health-dependencies-retriever health-dependencies-nv-ingest \
	health-dependencies-pdf-server health-dependencies-url-ingest health-dependencies-k8s-mcp \
	health-dependencies-unifi-mcp health-integration-hindsight health-integration-milvus health-integration-retriever \
	health-integration-nv-ingest health-integration-pdf-server health-integration-url-ingest \
	health-integration-k8s-mcp health-integration-unifi-mcp \
	healthcheck-hindsight healthcheck-milvus healthcheck-retriever healthcheck-nv-ingest healthcheck-pdf-server \
	healthcheck-url-ingest healthcheck-k8s-mcp healthcheck-unifi-mcp

$(CLUSTER_TARGETS): | require-context

.PHONY: refresh-run refresh-status refresh-verify
refresh-run refresh-status refresh-verify: | require-context
REFRESH_LIMIT ?= 0
MILVUS_DATABASE ?= default
refresh-run: ## [cluster/data write] Submit refresh Jobs for COLLECTIONS; REFRESH_LIMIT bounds data work. Does not wait.
	@for collection in $(COLLECTIONS); do \
	  DAEDALUS_NAMESPACE="$(DAEDALUS_NAMESPACE)" KUBECTL="$(KUBECTL)" \
	  python3 "$(ROOT_DIR)/scripts/refresh_job.py" --collection "$$collection" --limit "$(REFRESH_LIMIT)"; \
	done

refresh-status: ## [read-only cluster] Read persistent collection refresh status and freshness.
	@$(KUBECTL) -n "$(DAEDALUS_NAMESPACE)" exec deployment/url-ingest-monitor -- \
	  python3 -c 'import json,urllib.request; print(json.dumps(json.load(urllib.request.urlopen("http://localhost:8080/status")),indent=2))'

refresh-verify: ## [cluster write, data read-only] Submit a model/retrieval verification Job; inspect completion separately.
	@DAEDALUS_NAMESPACE="$(DAEDALUS_NAMESPACE)" KUBECTL="$(KUBECTL)" \
	  python3 "$(ROOT_DIR)/scripts/refresh_job.py" --collection kubernetes --verify

##@ Getting started

help: ## [read-only] Show supported workflows and their side effects.
	@printf '%s\n' 'Daedalus context Kubernetes operations'
	@awk ' \
	  /^##@ / { printf "\n%s\n", substr($$0, 5); next } \
	  /^[A-Za-z0-9_.-]+:.*## / { \
	    target = $$1; sub(/:.*/, "", target); \
	    description = $$0; sub(/^[^#]*## /, "", description); \
	    printf "  %-24s %s\n", target, description; \
	  }' "$(lastword $(MAKEFILE_LIST))"
	@printf '%s\n' \
	  '' \
	  'Required for every Kubernetes target: EXPECTED_CONTEXT=<approved literal context>' \
	  'Select components with: COMPONENTS="milvus retriever nv-ingest"' \
	  'Skip consumer probes before Daedalus exists with: CHECK_CONSUMER=false' \
	  'All releases use DAEDALUS_NAMESPACE=daedalus by default.'

doctor: ## [read-only cluster] Check tools, exported configuration, context, and cluster access.
	@[[ -f "$(ENV_FILE)" ]] || { \
	  echo "Missing $(ENV_FILE). Copy .env.example, edit it, then source it with export enabled." >&2; exit 1; \
	}
	@bash -n "$(ENV_FILE)"
	@for command in bash python3 "$(JQ)" "$(KUBECTL)" "$(HELM)"; do \
	  command -v "$$command" >/dev/null || { echo "Missing required command: $$command" >&2; exit 1; }; \
	done
	@[[ -n "$(MAKE_VERSION)" ]] || { echo 'GNU Make is required.' >&2; exit 1; }
	@[[ -n "$(K8S_MCP_IMAGE)" && -n "$(URL_INGEST_IMAGE)" ]] || { \
	  echo 'Root .env values are not exported to Make. Run: set -a; source .env; set +a' >&2; exit 1; \
	}
	@$(MAKE) --no-print-directory require-context
	@$(KUBECTL) cluster-info >/dev/null
	@echo 'Doctor checks passed. Next: make validate-cluster'

show-config: ## [read-only] Print effective non-secret operational configuration.
	@printf '%-24s %s\n' \
	  'components' '$(COMPONENTS)' \
	  'agent namespace' '$(AGENT_NAMESPACE)' \
	  'agent workload' '$(AGENT_WORKLOAD)' \
	  'milvus namespace' '$(MILVUS_NAMESPACE)' \
	  'milvus nodeports' '$(EXPOSE_NODEPORTS)' \
	  'milvus monitoring' '$(ENABLE_MILVUS_MONITORING)' \
	  'minio storage class' '$(MINIO_STORAGE_CLASS)' \
	  'etcd storage class' '$(ETCD_STORAGE_CLASS)' \
	  'workspace image version' '$(DAEDALUS_CONTEXT_IMAGE_VERSION)' \
	  'hindsight repository' '$(HINDSIGHT_ROOT)' \
	  'hindsight API' '$(HINDSIGHT_API_URL)' \
	  'nv-ingest version' '$(NV_INGEST_VERSION)' \
	  'nv-ingest namespace' '$(NV_INGEST_NAMESPACE)' \
	  'retriever namespace' '$(RETRIEVER_NAMESPACE)' \
	  'embedding model' '$(EMBEDDING_NIM_MODEL_NAME)' \
	  'reranker model' '$(RERANKER_MODEL_NAME)' \
	  'daedalus reranker API' '$(DAEDALUS_RERANKER_ENDPOINT)' \
	  'NIM reranker API' '$(NIM_RERANKER_ENDPOINT)' \
	  'scheduled ingest capacity' '$(URL_INGEST_GLOBAL_CAPACITY)' \
	  'seed source node' '$(URL_INGEST_SOURCE_NODE)' \
	  'pdf-server namespace' '$(PDF_SERVER_NAMESPACE)' \
	  'url-ingest namespace' '$(URL_INGEST_NAMESPACE)' \
	  'k8s-mcp namespace' '$(K8S_MCP_NAMESPACE)' \
	  'k8s-mcp nodeport' '$(K8S_MCP_NODE_PORT)' \
	  'k8s-mcp read-only' '$(K8S_MCP_READ_ONLY)' \
	  'k8s-mcp no-helm' '$(K8S_MCP_NO_HELM)' \
	  'k8s-mcp exec' '$(K8S_MCP_ENABLE_EXEC)' \
	  'k8s-mcp kubectl' '$(K8S_MCP_ENABLE_KUBECTL)' \
	  'k8s-mcp agent writes' '$(K8S_MCP_ENABLE_AGENT_WRITE)' \
	  'unifi-mcp namespace' '$(UNIFI_MCP_NAMESPACE)' \
	  'unifi-mcp nodeport' '$(UNIFI_MCP_NODE_PORT)' \
	  'MCP external clients' '$(MCP_EXTERNAL_CLIENT_CIDR)' \
	  'expected context' '$(or $(EXPECTED_CONTEXT),<required>)'

##@ Image lifecycle

images-validate: ## [read-only local] Validate all versioned image specifications.
	@command -v docker >/dev/null || { echo 'Missing required command: docker' >&2; exit 1; }
	@$(COMPOSE) version >/dev/null
	@$(COMPOSE) --project-directory "$(ROOT_DIR)" -f "$(COMPOSE_FILE)" \
	  --profile ingest config --quiet
	@source "$(ROOT_DIR)/scripts/image-reference.sh"; \
	  while IFS= read -r image; do \
	    require_semver_image_ref "Compose image" "$$image"; \
	  done < <($(COMPOSE) --project-directory "$(ROOT_DIR)" -f "$(COMPOSE_FILE)" \
	    --profile ingest config --images)

images-build: images-validate ## [local write] Build every locally maintained release image.
	@$(COMPOSE) --project-directory "$(ROOT_DIR)" -f "$(COMPOSE_FILE)" \
	  --profile ingest build $(IMAGE_BUILD_SERVICES)

images-push: images-build ## [registry write] Push every workspace release image.
	@$(COMPOSE) --project-directory "$(ROOT_DIR)" -f "$(COMPOSE_FILE)" \
	  --profile ingest push $(IMAGE_BUILD_SERVICES)

images-release-refs: images-validate ## [read-only local] Print Kubernetes image environment assignments.
	@command -v "$(JQ)" >/dev/null || { echo 'Missing required command: $(JQ)' >&2; exit 1; }
	@config="$$($(COMPOSE) --project-directory "$(ROOT_DIR)" -f "$(COMPOSE_FILE)" \
	  --profile ingest config --format json)"; \
	resolve_image() { \
	  variable="$$1"; \
	  service="$$2"; \
	  image="$$(printf '%s' "$$config" | $(JQ) -er --arg service "$$service" \
	    '.services[$$service].image')"; \
	  printf '%s=%s\n' "$$variable" "$$image"; \
	}; \
	resolve_image K8S_MCP_IMAGE k8s-mcp-server-read-only; \
	resolve_image UNIFI_MCP_IMAGE unifi-network-mcp-server; \
	resolve_image PDF_SERVER_IMAGE pdf-server; \
	resolve_image URL_INGEST_IMAGE url-ingest; \
	resolve_image URL_INGEST_RUNNER_IMAGE nv-ingest-ms-tasks; \
	resolve_image NV_INGEST_PARSER_IMAGE nv-ingest-parser

images-publish: images-push images-release-refs ## [registry write] Build, push, and print all release references.

# Backward-compatible image lifecycle aliases. These stay hidden from help.
compose-config: images-validate
compose-build: images-build
compose-push: images-push
compose-release-refs: images-release-refs
compose-publish: images-publish

##@ Kubernetes lifecycle

require-context:
	@command -v "$(KUBECTL)" >/dev/null || { echo "Missing kubectl: $(KUBECTL)" >&2; exit 1; }
	@[[ -n "$(strip $(EXPECTED_CONTEXT))" ]] || { \
	  echo 'EXPECTED_CONTEXT is required for every Kubernetes target.' >&2; exit 1; \
	}
	@current="$$($(KUBECTL) config current-context)"; \
	  if [[ "$$current" != "$(EXPECTED_CONTEXT)" ]]; then \
	    echo "Refusing context '$$current'; expected '$(EXPECTED_CONTEXT)'." >&2; exit 1; \
	  fi; \
	  echo "Kubernetes context: $$current"

preflight: ## [read-only cluster] Verify the guarded context and cluster access.
	@$(KUBECTL) cluster-info >/dev/null

verify-shared-secrets: preflight ## [read-only cluster] Verify externally managed Secrets required by all charts.
	@DAEDALUS_NAMESPACE="$(DAEDALUS_NAMESPACE)" KUBECTL="$(KUBECTL)" \
	  bash "$(ROOT_DIR)/scripts/check-shared-secrets.sh"

migrate-legacy-secrets: preflight ## [cluster write] Copy legacy provider Secrets into daedalus without exposing values.
	@DAEDALUS_NAMESPACE="$(DAEDALUS_NAMESPACE)" KUBECTL="$(KUBECTL)" JQ="$(JQ)" \
	  bash "$(ROOT_DIR)/scripts/migrate-legacy-secrets.sh"

migrate-hindsight: preflight ## [cluster/data write] Copy Hindsight into daedalus and verify the cutover fingerprints.
	@[[ -d "$(HINDSIGHT_ROOT)" ]] || { echo "Missing Hindsight repository: $(HINDSIGHT_ROOT)" >&2; exit 1; }
	@$(MAKE) --no-print-directory -C "$(HINDSIGHT_ROOT)" migrate-namespace \
	  NAMESPACE="$(DAEDALUS_NAMESPACE)" EXPECTED_CONTEXT="$(EXPECTED_CONTEXT)"

deploy-local-fast-storage: preflight ## [cluster write] Install the non-default local-fast StorageClass.
	@EXPECTED_CONTEXT="$(EXPECTED_CONTEXT)" \
	  bash "$(ROOT_DIR)/nim-milvus-ms/deploy-local-fast-storage.sh"

deploy-nfs-locking: preflight ## [cluster write] Enable server-coordinated locking for future UNAS NFS mounts.
	@EXPECTED_CONTEXT="$(EXPECTED_CONTEXT)" \
	  bash "$(ROOT_DIR)/nim-milvus-ms/deploy-nfs-locking.sh"

patch-nfs-pv-locking: preflight ## [cluster write] Converge existing UNAS PV mount options after canary validation.
	@EXPECTED_CONTEXT="$(EXPECTED_CONTEXT)" \
	  APPLY_NFS_PV_LOCKING="$(APPLY_NFS_PV_LOCKING)" \
	  bash "$(ROOT_DIR)/nim-milvus-ms/patch-unas-pv-locking.sh"

cleanup-legacy: preflight ## [destructive] Remove accepted legacy namespaces, RBAC, retained PVs, and exact local paths.
	@KUBECTL="$(KUBECTL)" HELM="$(HELM)" \
	  CONFIRM_LEGACY_CLEANUP="$(CONFIRM_LEGACY_CLEANUP)" \
	  bash "$(ROOT_DIR)/scripts/cleanup-legacy-stack.sh"

define run_components
	@for component in $(COMPONENTS); do \
	  printf '\n==> %s %s\n' '$(1)' "$$component"; \
	  $(MAKE) --no-print-directory "$(1)-$$component"; \
	done
endef

define check_components
	@failures=(); \
	filter_make_errors() { \
	  sed -u -E '/^(g?make)(\[[0-9]+\])?: \*\*\* .* Error [0-9]+$$/d'; \
	}; \
	for component in $(COMPONENTS); do \
	  printf '\n==> %s %s\n' '$(1)' "$$component"; \
	  if $(MAKE) --no-print-directory "$(1)-$$component" 2>&1 | filter_make_errors; then \
	    printf '<== %s %s: PASS\n' '$(1)' "$$component"; \
	  else \
	    failures+=("$$component"); \
	    printf '<== %s %s: FAIL\n' '$(1)' "$$component"; \
	  fi; \
	done; \
	total='$(words $(COMPONENTS))'; \
	if (( $${#failures[@]} )); then \
	  printf '\n%s summary: FAIL (%d/%d): %s\n' \
	    '$(1)' "$${#failures[@]}" "$$total" "$${failures[*]}"; \
	  exit 1; \
	fi; \
	printf '\n%s summary: PASS (%s/%s)\n' '$(1)' "$$total" "$$total"
endef

deploy: ## [cluster write] Load configuration, validate, reconcile, and check selected components.
	@[[ -f "$(ENV_FILE)" ]] || { \
	  echo "Missing $(ENV_FILE). Copy .env.example and edit it before deployment." >&2; exit 1; \
	}
	@bash -n "$(ENV_FILE)"
	@set -a; \
	  source "$(ENV_FILE)"; \
	  set +a; \
	  $(MAKE) --no-print-directory deploy-workflow

deploy-workflow:
	@$(MAKE) --no-print-directory doctor
	@$(MAKE) --no-print-directory show-config
	@$(MAKE) --no-print-directory validate-cluster
	@$(MAKE) --no-print-directory verify-shared-secrets
	@"$(ROOT_DIR)/scripts/contextctl" operate --action deploy --components $(COMPONENTS)
	@$(MAKE) --no-print-directory health-provider

enable-schedules: preflight ## [cluster write] Enable URL ingestion schedules after corpus acceptance.
	@$(MAKE) --no-print-directory deploy-url-ingest URL_INGEST_SUSPEND=false

enable-external-mcp: preflight ## [cluster write] Enable authenticated MCP NodePorts after internal acceptance.
	@$(MAKE) --no-print-directory deploy-k8s-mcp MCP_EXTERNAL_ENABLED=true
	@$(MAKE) --no-print-directory deploy-unifi-mcp MCP_EXTERNAL_ENABLED=true

update: preflight ## [cluster write] Reconcile selected already-published release images.
	@"$(ROOT_DIR)/scripts/contextctl" operate --action update --components $(COMPONENTS)

# Public convenience targets load the same root configuration as deploy.
define load_env_and_run
	@[[ -f "$(ENV_FILE)" ]] || { echo "Missing $(ENV_FILE)." >&2; exit 1; }
	@bash -n "$(ENV_FILE)"
	@set -a; source "$(ENV_FILE)"; set +a; $(MAKE) --no-print-directory $(1)
endef

restart: ## [cluster write] Load .env and restart all selected services using existing storage.
	$(call load_env_and_run,redeploy)

recovery-status: ## [read-only cluster] Load .env and report restart and storage recovery blockers.
	$(call load_env_and_run,recovery-status-workflow)

recovery-status-workflow: preflight
	@if [[ -n "$(strip $(RECOVERY_RELEASE_ARGS))" ]]; then \
	  KUBECTL="$(KUBECTL)" python3 "$(ROOT_DIR)/scripts/workload_recovery.py" status \
	    --context "$(EXPECTED_CONTEXT)" --namespace "$(DAEDALUS_NAMESPACE)" $(RECOVERY_RELEASE_ARGS); \
	else echo 'URL ingestion has no long-running workloads; use make health-provider COMPONENTS=url-ingest.'; fi

redeploy: preflight ## [cluster write] Restart selected workloads without deleting durable data.
	@"$(ROOT_DIR)/scripts/contextctl" operate --action redeploy --components $(COMPONENTS)

##@ Health and diagnostics

health-provider: preflight ## [read-only cluster] Check provider workloads without requiring Daedalus.
	$(call check_components,health-provider)

health-dependencies: preflight ## [read-only cluster] Check dependencies, persistence, and data freshness.
	$(call check_components,health-dependencies)

health-integration: preflight healthcheck-agent ## [read-only cluster] Probe selected providers from Daedalus.
	$(call check_components,health-integration)

health-stack: ## [read-only cluster] Print combined availability, freshness, and redundancy JSON.
	@"$(ROOT_DIR)/scripts/contextctl" health-stack --environment "$(ENVIRONMENT)" \
	  --components $(COMPONENTS)

healthcheck: health-stack

healthcheck-agent:
	@$(KUBECTL) get namespace "$(AGENT_NAMESPACE)" >/dev/null
	@$(KUBECTL) --namespace="$(AGENT_NAMESPACE)" rollout status "$(AGENT_WORKLOAD)" --timeout="$(ROLLOUT_TIMEOUT)"

# Deployment -----------------------------------------------------------------

deploy-hindsight:
	@[[ -d "$(HINDSIGHT_ROOT)" ]] || { echo "Missing Hindsight repository: $(HINDSIGHT_ROOT)" >&2; exit 1; }
	@$(MAKE) --no-print-directory -C "$(HINDSIGHT_ROOT)" deploy \
	  NAMESPACE="$(DAEDALUS_NAMESPACE)" EXPECTED_CONTEXT="$(EXPECTED_CONTEXT)"

deploy-retriever:
	@values_args=(); \
	if [[ -n "$(RETRIEVER_VALUES_FILE)" ]]; then \
	  [[ -f "$(RETRIEVER_VALUES_FILE)" ]] || { \
	    echo "Retriever values file not found: $(RETRIEVER_VALUES_FILE)" >&2; exit 1; \
	  }; \
	  values_args+=(--values "$(RETRIEVER_VALUES_FILE)"); \
	fi; \
	$(HELM) upgrade --install "$(RETRIEVER_RELEASE)" "$(RETRIEVER_CHART)" \
	  --namespace "$(RETRIEVER_NAMESPACE)" --create-namespace \
	  --set-string fullnameOverride="$(RETRIEVER_FULLNAME)" \
	  "$${values_args[@]}" --atomic --wait --history-max 10 \
	  --timeout "$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	$(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" rollout status deployment \
	  --selector="app.kubernetes.io/instance=$(RETRIEVER_RELEASE)" \
	  --timeout="$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	if $(KUBECTL) api-resources --api-group=cilium.io --output=name 2>/dev/null \
	    | grep -Fxq 'ciliumnetworkpolicies.cilium.io'; then \
	  mapfile -t cilium_policies < <($(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" \
	    get ciliumnetworkpolicy \
	    --selector="app.kubernetes.io/instance=$(RETRIEVER_RELEASE),daedalus.ai/capability=retrieval" \
	    --output=name); \
	  for policy in "$${cilium_policies[@]}"; do \
	    $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" wait --for=condition=Valid "$$policy" \
	      --timeout="$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	  done; \
	fi

deploy-milvus:
	@[[ -n "$(MINIO_STORAGE_CLASS)" ]] || { echo 'MINIO_STORAGE_CLASS is required.' >&2; exit 1; }
	@$(HELM) upgrade --install "$(MILVUS_RELEASE)" "$(MILVUS_CHART)" \
	  --namespace "$(DAEDALUS_NAMESPACE)" --create-namespace \
	  --post-renderer "$(ROOT_DIR)/nim-milvus-ms/chart/post-render.py" \
	  --set-string milvus.minio.persistence.storageClass="$(MINIO_STORAGE_CLASS)" \
	  --set-string milvus.etcd.persistence.storageClass="$(ETCD_STORAGE_CLASS)" \
	  --set milvus.metrics.serviceMonitor.enabled="$(ENABLE_MILVUS_MONITORING)" \
	  --atomic --wait --history-max 10 --timeout "$(ROLLOUT_TIMEOUT)"

deploy-nv-ingest:
	@[[ -n "$(EFFECTIVE_NV_INGEST_IMAGE_STORAGE_URI)" ]] || { \
	  echo 'IMAGE_STORAGE_URI is required for NV-Ingest.' >&2; exit 1; \
	}
	@$(HELM) upgrade --install "$(NV_INGEST_RELEASE)" "$(NV_INGEST_CHART)" \
	  --namespace "$(DAEDALUS_NAMESPACE)" --create-namespace \
	  --post-renderer "$(ROOT_DIR)/nv-ingest/helm-post-renderer.sh" \
	  --set-string nv-ingest.image.tag="$(NV_INGEST_VERSION)" \
	  --set nv-ingest.replicaCount="$(NV_INGEST_REPLICA_COUNT)" \
	  --set-string localParser.image="$(NV_INGEST_PARSER_IMAGE)" \
	  --set-string nv-ingest.envVars.LOCAL_DOCUMENT_PARSE_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)" \
	  --set-string nv-ingest.envVars.LOCAL_DOCUMENT_PARSE_REVISION="$$(sha256sum '$(NV_INGEST_CHART)/files/local_pdf.py' | cut -d' ' -f1)" \
	  --set-string nv-ingest.envVars.MILVUS_ENDPOINT="$(MILVUS_ENDPOINT)" \
	  --set-string nv-ingest.envVars.MINIO_INTERNAL_ADDRESS="$(MINIO_ENDPOINT)" \
	  --set-string nv-ingest.envVars.MINIO_PUBLIC_ADDRESS="$(MINIO_ENDPOINT)" \
	  --set-string nv-ingest.envVars.MINIO_BUCKET="$(MINIO_BUCKET)" \
	  --set-string nv-ingest.envVars.EMBEDDING_NIM_ENDPOINT="$(EMBEDDING_NIM_ENDPOINT)" \
	  --set-string nv-ingest.envVars.EMBEDDING_NIM_MODEL_NAME="$(EMBEDDING_NIM_MODEL_NAME)" \
	  --set-string nv-ingest.envVars.OCR_HTTP_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled" \
	  --set-string nv-ingest.envVars.OCR_INFER_PROTOCOL="$(OCR_INFER_PROTOCOL)" \
	  --set-string nv-ingest.envVars.OCR_MODEL_NAME="$(OCR_MODEL_NAME)" \
	  --set-string nv-ingest.envVars.YOLOX_HTTP_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled" \
	  --set-string nv-ingest.envVars.YOLOX_INFER_PROTOCOL="$(YOLOX_INFER_PROTOCOL)" \
	  --set-string nv-ingest.envVars.YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled" \
	  --set-string nv-ingest.envVars.YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL="$(YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL)" \
	  --set-string nv-ingest.envVars.YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled" \
	  --set-string nv-ingest.envVars.YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL="$(YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL)" \
	  --set-string nv-ingest.envVars.VLM_CAPTION_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled" \
	  --set-string nv-ingest.envVars.VLM_CAPTION_MODEL_NAME="$(VLM_CAPTION_MODEL_NAME)" \
	  --set-string nv-ingest.envVars.NEMOTRON_PARSE_HTTP_ENDPOINT="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled" \
	  --set-string nv-ingest.envVars.NEMOTRON_PARSE_INFER_PROTOCOL="$(NEMOTRON_PARSE_INFER_PROTOCOL)" \
	  --set-string nv-ingest.envVars.NEMOTRON_PARSE_MODEL_NAME="$(NEMOTRON_PARSE_MODEL_NAME)" \
	  --set-string nv-ingest.envVars.IMAGE_STORAGE_URI="$(EFFECTIVE_NV_INGEST_IMAGE_STORAGE_URI)" \
	  --atomic --wait --history-max 10 --timeout "$(ROLLOUT_TIMEOUT)"

deploy-pdf-server:
	@image='$(PDF_SERVER_IMAGE)'; repository="$${image%:*}"; tag="$${image##*:}"; \
	  $(HELM) upgrade --install pdf-server "$(PDF_SERVER_CHART)" \
	    --namespace "$(DAEDALUS_NAMESPACE)" --create-namespace \
	    --set-string image.repository="$$repository" --set-string image.tag="$$tag" \
	    --set-string persistence.storageClass="$(PDF_STORAGE_CLASS)" \
	    --set-string persistence.size="$(PDF_STORAGE_SIZE)" \
	    --atomic --wait --history-max 10 --timeout "$(ROLLOUT_TIMEOUT)"

deploy-url-ingest:
	@python3 "$(ROOT_DIR)/scripts/render_refresh_contract.py" --output "$(URL_INGEST_CHART)/files/refresh-contract.json"
	@cp "$(ROOT_DIR)/url-ingest/refresh_monitor.py" "$(URL_INGEST_CHART)/files/refresh_monitor.py"
	@$(HELM) upgrade --install url-ingest "$(URL_INGEST_CHART)" \
	  --namespace "$(DAEDALUS_NAMESPACE)" --create-namespace \
	  --set-string images.crawler="$(URL_INGEST_IMAGE)" \
	  --set-string images.runner="$(URL_INGEST_RUNNER_IMAGE)" \
	  --set schedules.suspend="$(URL_INGEST_SUSPEND)" \
	  --set ingestion.globalCapacity="$(URL_INGEST_GLOBAL_CAPACITY)" \
	  --set-string endpoints.embedding="$(EMBEDDING_NIM_ENDPOINT)" \
	  --set monitoring.enabled=true \
	  --set-file urlLists.kubernetes="$(ROOT_DIR)/url-ingest/files/k8s_url_links.txt" \
	  --set-file urlLists.vetpartner="$(ROOT_DIR)/url-ingest/files/vetpartner_url_links.txt" \
	  --atomic --wait --history-max 10 --timeout "$(ROLLOUT_TIMEOUT)"

deploy-k8s-mcp:
	@image='$(K8S_MCP_IMAGE)'; repository="$${image%:*}"; tag="$${image##*:}"; external_args=(); helm_args=(); \
	  if [[ -n "$(K8S_MCP_VALUES_FILE)" ]]; then \
	    helm_args+=(--values "$(K8S_MCP_VALUES_FILE)"); \
	  fi; \
	  if [[ "$(MCP_EXTERNAL_ENABLED)" == true ]]; then \
	    external_args+=(--set-string 'service.external.sourceRanges[0]=$(MCP_EXTERNAL_CLIENT_CIDR)'); \
	  else \
	    helm_args+=(--atomic --wait); \
	  fi; \
	  $(HELM) upgrade --install k8s-mcp-server "$(K8S_MCP_CHART)" \
	    --namespace "$(DAEDALUS_NAMESPACE)" --create-namespace \
	    --set-string image.repository="$$repository" --set-string image.tag="$$tag" \
	    --set server.readOnly="$(K8S_MCP_READ_ONLY)" --set server.noHelm="$(K8S_MCP_NO_HELM)" \
	    --set server.enableExec="$(K8S_MCP_ENABLE_EXEC)" \
	    --set server.enableKubectl="$(K8S_MCP_ENABLE_KUBECTL)" \
	    --set server.enableAgentWrite="$(K8S_MCP_ENABLE_AGENT_WRITE)" \
	    --set service.external.enabled="$(MCP_EXTERNAL_ENABLED)" \
	    --set service.external.nodePort="$(K8S_MCP_NODE_PORT)" "$${external_args[@]}" \
	    "$${helm_args[@]}" --history-max 10 --timeout "$(ROLLOUT_TIMEOUT)"; \
	  if [[ "$(MCP_EXTERNAL_ENABLED)" == true ]]; then \
	    $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" rollout status \
	      deployment/k8s-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"; \
	    $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" get service/k8s-mcp-server -o json | \
	      python3 "$(ROOT_DIR)/scripts/verify-external-mcp-service.py" \
	        --node-port "$(K8S_MCP_NODE_PORT)" --source-range "$(MCP_EXTERNAL_CLIENT_CIDR)"; \
	  fi

deploy-unifi-mcp:
	@image='$(UNIFI_MCP_IMAGE)'; repository="$${image%:*}"; tag="$${image##*:}"; external_args=(); helm_args=(); \
	  if [[ -n "$(UNIFI_CONTROLLER_CA_FILE)" ]]; then \
	    helm_args+=(--set-file "controller.caBundle=$(UNIFI_CONTROLLER_CA_FILE)"); \
	  fi; \
	  if [[ -n "$(UNIFI_CONTROLLER_HOSTNAME)$(UNIFI_CONTROLLER_IP)" ]]; then \
	    [[ -n "$(UNIFI_CONTROLLER_HOSTNAME)" && -n "$(UNIFI_CONTROLLER_IP)" ]] || \
	      { echo "UNIFI_CONTROLLER_HOSTNAME and UNIFI_CONTROLLER_IP must be set together" >&2; exit 1; }; \
	    helm_args+=(--set-string 'controller.hostAliases[0].ip=$(UNIFI_CONTROLLER_IP)' \
	      --set-string 'controller.hostAliases[0].hostnames[0]=$(UNIFI_CONTROLLER_HOSTNAME)'); \
	  fi; \
	  if [[ "$(MCP_EXTERNAL_ENABLED)" == true ]]; then \
	    external_args+=(--set-string 'service.external.sourceRanges[0]=$(MCP_EXTERNAL_CLIENT_CIDR)'); \
	  else \
	    helm_args+=(--atomic --wait); \
	  fi; \
	  $(HELM) upgrade --install unifi-network-mcp-server "$(UNIFI_MCP_CHART)" \
	    --namespace "$(DAEDALUS_NAMESPACE)" --create-namespace \
	    --set-string image.repository="$$repository" --set-string image.tag="$$tag" \
	    --set-string controller.baseUrl="$(UNIFI_BASE_URL)" \
	    --set controller.insecureSkipVerify="$(UNIFI_INSECURE_SKIP_VERIFY)" \
	    --set-string controller.cidr="$(UNIFI_CONTROLLER_CIDR)" \
	    --set service.external.enabled="$(MCP_EXTERNAL_ENABLED)" \
	    --set service.external.nodePort="$(UNIFI_MCP_NODE_PORT)" "$${external_args[@]}" \
	    "$${helm_args[@]}" --history-max 10 --timeout "$(ROLLOUT_TIMEOUT)"; \
	  if [[ "$(MCP_EXTERNAL_ENABLED)" == true ]]; then \
	    $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" rollout status \
	      deployment/unifi-network-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"; \
	    $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" get \
	      service/unifi-network-mcp-server -o json | \
	      python3 "$(ROOT_DIR)/scripts/verify-external-mcp-service.py" \
	        --node-port "$(UNIFI_MCP_NODE_PORT)" --source-range "$(MCP_EXTERNAL_CLIENT_CIDR)"; \
	  fi

# Updates --------------------------------------------------------------------

update-hindsight: deploy-hindsight
update-milvus: deploy-milvus
update-retriever: deploy-retriever
update-nv-ingest: deploy-nv-ingest
update-url-ingest: deploy-url-ingest

update-pdf-server: deploy-pdf-server
	@$(MAKE) --no-print-directory redeploy-pdf-server

update-k8s-mcp: deploy-k8s-mcp
update-unifi-mcp: deploy-unifi-mcp

# Restarts -------------------------------------------------------------------

define restart_release
	@KUBECTL="$(KUBECTL)" python3 "$(ROOT_DIR)/scripts/workload_recovery.py" restart \
	  --context "$(EXPECTED_CONTEXT)" --namespace "$(DAEDALUS_NAMESPACE)" \
	  --release "$(1)" --timeout "$(2)"
endef

redeploy-hindsight:
	$(call restart_release,$(HINDSIGHT_RELEASE),$(ROLLOUT_TIMEOUT))

redeploy-milvus:
	$(call restart_release,$(MILVUS_RELEASE),$(ROLLOUT_TIMEOUT))

redeploy-retriever:
	$(call restart_release,$(RETRIEVER_RELEASE),$(RETRIEVER_ROLLOUT_TIMEOUT))

redeploy-nv-ingest:
	$(call restart_release,$(NV_INGEST_RELEASE),$(ROLLOUT_TIMEOUT))

redeploy-pdf-server:
	$(call restart_release,pdf-server,$(ROLLOUT_TIMEOUT))

redeploy-url-ingest:
	@echo "CronJobs have no persistent pods; existing schedules and Jobs are unchanged. Use make update COMPONENTS=url-ingest to reconcile templates."

redeploy-k8s-mcp:
	$(call restart_release,k8s-mcp-server,$(ROLLOUT_TIMEOUT))

redeploy-unifi-mcp:
	$(call restart_release,unifi-network-mcp-server,$(ROLLOUT_TIMEOUT))

# Health checks --------------------------------------------------------------

define assert_bound_pvcs
	@pvc_rows="$$($(KUBECTL) --namespace="$(1)" get pvc --selector="$(2)" \
	  -o custom-columns='NAME:.metadata.name,PHASE:.status.phase' --no-headers)"; \
	if [[ -z "$$pvc_rows" ]]; then echo "No PVCs found for selector $(2) in $(1)." >&2; exit 1; fi; \
	unbound="$$(awk '$$2 != "Bound" {print $$1 "=" $$2}' <<<"$$pvc_rows")"; \
	if [[ -n "$$unbound" ]]; then echo "Unbound PVCs: $$unbound" >&2; exit 1; fi
endef

define assert_resource_count
	@resources="$$($(KUBECTL) --namespace="$(1)" get "$(2)" --selector="$(3)" -o name)"; \
	count="$$(grep -c . <<<"$$resources" || true)"; \
	if [[ "$$count" -ne "$(4)" ]]; then \
	  printf 'Expected %s %s resources for selector %s in %s; found %s.\n%s\n' \
	    '$(4)' '$(2)' '$(3)' '$(1)' "$$count" "$$resources" >&2; \
	  exit 1; \
	fi
endef

define assert_nonempty_resources
	@resources="$$($(KUBECTL) --namespace="$(1)" get "$(2)" --selector="$(3)" -o name)"; \
	[[ -n "$$resources" ]] || { \
	  printf 'No %s resources found for selector %s in %s.\n' '$(2)' '$(3)' '$(1)' >&2; \
	  exit 1; \
	}
endef

define consumer_http
	@if [[ "$(CHECK_CONSUMER)" == "true" ]]; then \
	  echo "Agent probe: $(1)"; \
	  probe_ok=false; probe_rc=1; probe_output=""; \
	  for ((attempt=0; attempt <= $(CONSUMER_RETRIES); attempt++)); do \
	    if probe_output="$$($(KUBECTL) --namespace="$(AGENT_NAMESPACE)" exec "$(AGENT_WORKLOAD)" -- \
	        python3 -c 'import urllib.request; response = urllib.request.urlopen("$(1)", timeout=20); response.read(1)' \
	        2>&1)"; then \
	      probe_ok=true; break; \
	    else \
	      probe_rc="$$?"; \
	    fi; \
	    if (( attempt < $(CONSUMER_RETRIES) )); then sleep 1; fi; \
	  done; \
	  if [[ "$$probe_ok" == "true" ]]; then \
	    echo "Agent probe passed: $(1)"; \
	  else \
	    detail="$$(printf '%s\n' "$$probe_output" | \
	      sed -E '/^command terminated with exit code [0-9]+$$/d' | tail -n 1)"; \
	    [[ -n "$$detail" ]] || detail="probe exited with status $$probe_rc"; \
	    printf 'FAIL consumer HTTP: %s in namespace %s -> %s: %s\n' \
	      '$(AGENT_WORKLOAD)' '$(AGENT_NAMESPACE)' '$(1)' "$$detail" >&2; \
	    exit 1; \
	  fi; \
	fi
endef

define consumer_tcp
	@if [[ "$(CHECK_CONSUMER)" == "true" ]]; then \
	  echo "Agent probe: $(1):$(2)"; \
	  probe_ok=false; probe_rc=1; probe_output=""; \
	  for ((attempt=0; attempt <= $(CONSUMER_RETRIES); attempt++)); do \
	    if probe_output="$$($(KUBECTL) --namespace="$(AGENT_NAMESPACE)" exec "$(AGENT_WORKLOAD)" -- \
	        python3 -c 'import socket; socket.create_connection(("$(1)", $(2)), 5).close()' 2>&1)"; then \
	      probe_ok=true; break; \
	    else \
	      probe_rc="$$?"; \
	    fi; \
	    if (( attempt < $(CONSUMER_RETRIES) )); then sleep 1; fi; \
	  done; \
	  if [[ "$$probe_ok" == "true" ]]; then \
	    echo "Agent probe passed: $(1):$(2)"; \
	  else \
	    detail="$$(printf '%s\n' "$$probe_output" | \
	      sed -E '/^command terminated with exit code [0-9]+$$/d' | tail -n 1)"; \
	    [[ -n "$$detail" ]] || detail="probe exited with status $$probe_rc"; \
	    printf 'FAIL consumer TCP: %s in namespace %s -> %s:%s: %s\n' \
	      '$(AGENT_WORKLOAD)' '$(AGENT_NAMESPACE)' '$(1)' '$(2)' "$$detail" >&2; \
	    exit 1; \
	  fi; \
	fi
endef

define report_milvus_collections
	@command -v "$(JQ)" >/dev/null || { echo "Missing jq: $(JQ)" >&2; exit 1; }; \
	proxy_pod="$$($(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" get pods \
	  --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE),component=proxy" \
	  --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}')"; \
	[[ -n "$$proxy_pod" ]] || { echo "No running Milvus proxy pod found." >&2; exit 1; }; \
	milvus_api() { \
	  local path="$$1" body="$$2"; \
	  body="$$($(JQ) -c --arg dbName "$(MILVUS_DATABASE)" '. + {dbName: $$dbName}' <<<"$$body")"; \
	  $(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" get secret "$(MILVUS_ROOT_SECRET_NAME)" \
	    -o jsonpath='{.data.password}' | base64 --decode | \
	    $(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" exec -i "$$proxy_pod" -- \
	      sh -c 'IFS= read -r password; \
	        curl --connect-timeout 5 --max-time 30 --fail --silent --show-error \
	          --header "Authorization: Bearer root:$$password" \
	          --header "Content-Type: application/json" \
	          --data "$$1" "http://127.0.0.1:19530/v2/vectordb/$$2"' \
	      sh "$$body" "$$path"; \
	}; \
	if ! list_response="$$(milvus_api collections/list '{}')"; then \
	  echo "Failed to list Milvus collections." >&2; exit 1; \
	fi; \
	if ! "$(JQ)" -e '.code == 0 and (.data | type == "array")' \
	    >/dev/null <<<"$$list_response"; then \
	  printf 'Invalid Milvus collection-list response: %s\n' "$$list_response" >&2; exit 1; \
	fi; \
	mapfile -t collections < <("$(JQ)" -r '.data[]' <<<"$$list_response" | LC_ALL=C sort); \
	declare -A collection_rows=(); \
	printf '\nAvailable Milvus collections and record counts:\n'; \
	if (( $${#collections[@]} == 0 )); then \
	  printf '  (none)\n'; \
	fi; \
	for collection in "$${collections[@]}"; do \
	  body="$$($(JQ) -cn --arg collectionName "$$collection" \
	    '{collectionName: $$collectionName}')"; \
	  if ! stats_response="$$(milvus_api collections/get_stats "$$body")"; then \
	    printf 'Failed to get Milvus statistics for collection %s.\n' "$$collection" >&2; exit 1; \
	  fi; \
	  if ! row_count="$$($(JQ) -er 'select(.code == 0) | .data.rowCount | tonumber' \
	      <<<"$$stats_response")"; then \
	    printf 'Invalid Milvus statistics response for %s: %s\n' \
	      "$$collection" "$$stats_response" >&2; exit 1; \
	  fi; \
	  collection_rows["$$collection"]="$$row_count"; \
	  printf '  %s: %s records\n' "$$collection" "$$row_count"; \
	done; \
	required_failures=(); \
	for required in $(REQUIRED_COLLECTIONS); do \
	  rows="$${collection_rows[$$required]:-}"; \
	  if [[ -z "$$rows" ]]; then \
	    required_failures+=("$$required=missing"); \
	  elif (( rows <= 0 )); then \
	    required_failures+=("$$required=empty"); \
	  fi; \
	done; \
	if (( $${#required_failures[@]} )); then \
	  printf 'Required collection gate failed: %s\n' "$${required_failures[*]}" >&2; \
	  exit 1; \
	fi
endef

health-provider-hindsight:
	@$(HELM) --namespace="$(DAEDALUS_NAMESPACE)" status "$(HINDSIGHT_RELEASE)" >/dev/null
	@$(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" rollout status statefulset/hindsight-postgresql --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" rollout status deployment/hindsight-api --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" rollout status deployment/hindsight-control-plane --timeout="$(ROLLOUT_TIMEOUT)"

health-dependencies-hindsight:
	@[[ -d "$(HINDSIGHT_ROOT)" ]] || { echo "Missing Hindsight repository: $(HINDSIGHT_ROOT)" >&2; exit 1; }
	@$(MAKE) --no-print-directory -C "$(HINDSIGHT_ROOT)" health \
	  NAMESPACE="$(DAEDALUS_NAMESPACE)" EXPECTED_CONTEXT="$(EXPECTED_CONTEXT)"

health-integration-hindsight:
	$(call consumer_http,$(HINDSIGHT_API_URL)/health)

health-provider-milvus:
	@$(HELM) --namespace="$(MILVUS_NAMESPACE)" status "$(MILVUS_RELEASE)" >/dev/null
	$(call assert_nonempty_resources,$(MILVUS_NAMESPACE),deployment,app.kubernetes.io/instance=$(MILVUS_RELEASE))
	$(call assert_nonempty_resources,$(MILVUS_NAMESPACE),statefulset,app.kubernetes.io/instance=$(MILVUS_RELEASE))
	$(call assert_nonempty_resources,$(MILVUS_NAMESPACE),statefulset,release=$(MILVUS_RELEASE))
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status deployment --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status statefulset --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status statefulset --selector="release=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	$(call assert_bound_pvcs,$(MILVUS_NAMESPACE),app.kubernetes.io/instance=$(MILVUS_RELEASE))
	$(call assert_bound_pvcs,$(MILVUS_NAMESPACE),release=$(MILVUS_RELEASE))

health-dependencies-milvus:
	@$(KUBECTL) get --raw "/api/v1/namespaces/$(MILVUS_NAMESPACE)/services/http:$(MILVUS_SERVICE):9091/proxy/healthz" >/dev/null
	@$(KUBECTL) get --raw "/api/v1/namespaces/$(MILVUS_NAMESPACE)/services/http:$(MINIO_SERVICE):9000/proxy/minio/health/live" >/dev/null
	@if [[ "$(ENABLE_MILVUS_MONITORING)" == "true" ]]; then \
	  $(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" get servicemonitor "$(MILVUS_RELEASE)" >/dev/null; \
	fi
	$(call report_milvus_collections)

health-integration-milvus:
	$(call consumer_tcp,$(MILVUS_SERVICE).$(MILVUS_NAMESPACE).svc.cluster.local,19530)
	$(call consumer_http,$(MINIO_ENDPOINT)/minio/health/live)

health-provider-retriever:
	@$(HELM) --namespace="$(RETRIEVER_NAMESPACE)" status "$(RETRIEVER_RELEASE)" >/dev/null
	$(call assert_resource_count,$(RETRIEVER_NAMESPACE),deployment,app.kubernetes.io/instance=$(RETRIEVER_RELEASE),2)
	@for deployment in "$(RETRIEVER_FULLNAME)-embedder" "$(RETRIEVER_FULLNAME)-reranker"; do \
	  $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" rollout status "deployment/$$deployment" \
	    --timeout="$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	done

health-dependencies-retriever:
	@$(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" exec \
	  deployment/$(RETRIEVER_EMBEDDING_SERVICE) -c embedder -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/adapter-health >/dev/null
	@$(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" exec \
	  deployment/$(RETRIEVER_RERANKER_SERVICE) -c reranker -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/adapter-health >/dev/null
	@$(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" exec \
	  deployment/$(RETRIEVER_EMBEDDING_SERVICE) -c embedder -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/health >/dev/null
	@$(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" exec \
	  deployment/$(RETRIEVER_RERANKER_SERVICE) -c reranker -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/health >/dev/null

health-integration-retriever:
	$(call consumer_http,http://$(RETRIEVER_EMBEDDING_SERVICE).$(RETRIEVER_NAMESPACE).svc.cluster.local:8000/health)
	$(call consumer_http,http://$(RETRIEVER_RERANKER_SERVICE).$(RETRIEVER_NAMESPACE).svc.cluster.local:8000/health)
	@$(KUBECTL) --namespace="$(AGENT_NAMESPACE)" exec -i "$(AGENT_WORKLOAD)" -- \
	  env \
	    EMBEDDING_URL="$(EMBEDDING_NIM_ENDPOINT)/embeddings" \
	    NATIVE_RERANKER_URL="$(DAEDALUS_RERANKER_ENDPOINT)" \
	    NIM_RERANKER_URL="$(NIM_RERANKER_ENDPOINT)" \
	    EMBEDDING_MODEL="$(EMBEDDING_NIM_MODEL_NAME)" \
	    RERANKER_MODEL="$(RERANKER_MODEL_NAME)" \
	    EXPECTED_DIMENSION=2048 python3 - < "$(ROOT_DIR)/scripts/check_retriever_contract.py"

health-provider-nv-ingest:
	@$(HELM) --namespace="$(NV_INGEST_NAMESPACE)" status "$(NV_INGEST_RELEASE)" >/dev/null
	$(call assert_nonempty_resources,$(NV_INGEST_NAMESPACE),deployment,app.kubernetes.io/instance=$(NV_INGEST_RELEASE))
	$(call assert_nonempty_resources,$(NV_INGEST_NAMESPACE),statefulset,app.kubernetes.io/instance=$(NV_INGEST_RELEASE))
	@$(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" rollout status deployment --selector="app.kubernetes.io/instance=$(NV_INGEST_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" rollout status statefulset --selector="app.kubernetes.io/instance=$(NV_INGEST_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	$(call assert_bound_pvcs,$(NV_INGEST_NAMESPACE),app.kubernetes.io/instance=$(NV_INGEST_RELEASE))

health-dependencies-nv-ingest:
	@$(KUBECTL) get --raw "/api/v1/namespaces/$(NV_INGEST_NAMESPACE)/services/http:$(NV_INGEST_SERVICE):7670/proxy/v1/health/ready" >/dev/null
	@$(KUBECTL) get --raw "/api/v1/namespaces/$(NV_INGEST_NAMESPACE)/services/http:nv-ingest-parser:8000/proxy/readyz" >/dev/null
	@for pod in $$($(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" get pods \
	  --selector="app.kubernetes.io/name=nv-ingest,app.kubernetes.io/instance=$(NV_INGEST_RELEASE)" -o name); do \
	  $(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" exec "$$pod" -- python3 -c \
	    'import os, urllib.request; urllib.request.urlopen(os.environ["LOCAL_DOCUMENT_PARSE_ENDPOINT"].rstrip("/") + "/readyz", timeout=10).close()'; \
	done

health-integration-nv-ingest:
	$(call consumer_http,$(NV_INGEST_ENDPOINT)/v1/health/ready)

health-provider-pdf-server:
	@$(HELM) --namespace="$(DAEDALUS_NAMESPACE)" status pdf-server >/dev/null
	@$(KUBECTL) --namespace="$(PDF_SERVER_NAMESPACE)" rollout status deployment/pdf-server --timeout="$(ROLLOUT_TIMEOUT)"
	$(call assert_bound_pvcs,$(PDF_SERVER_NAMESPACE),app.kubernetes.io/name=pdf-server)

health-dependencies-pdf-server:
	@$(KUBECTL) --namespace="$(PDF_SERVER_NAMESPACE)" exec deployment/pdf-server -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/pdfs/ >/dev/null
	@$(KUBECTL) --namespace="$(PDF_SERVER_NAMESPACE)" exec deployment/pdf-server -- \
	  sh -c 'test -s /usr/share/nginx/pdfs/current-generation || { \
	      echo "PDF corpus generation marker is missing; publish the legacy corpus into the generation layout." >&2; \
	      exit 1; \
	    }; \
	    find /usr/share/nginx/pdfs -type f -name "*.pdf" -print -quit | grep -q . || { \
	      echo "PDF corpus contains no PDF files." >&2; \
	      exit 1; \
	    }'

health-integration-pdf-server:
	$(call consumer_http,http://pdf-server.$(PDF_SERVER_NAMESPACE).svc.cluster.local/pdfs/)

health-provider-url-ingest:
	@$(HELM) --namespace="$(DAEDALUS_NAMESPACE)" status url-ingest >/dev/null
	$(call assert_bound_pvcs,$(URL_INGEST_NAMESPACE),app.kubernetes.io/name=url-ingest)
	$(call assert_resource_count,$(URL_INGEST_NAMESPACE),cronjob,app.kubernetes.io/name=url-ingest,5)

health-dependencies-url-ingest:
	@NAMESPACE="$(URL_INGEST_NAMESPACE)" COLLECTIONS="$(REQUIRED_COLLECTIONS)" \
	  RUNNING_GRACE_SECONDS="$(URL_INGEST_RUNNING_GRACE_SECONDS)" KUBECTL="$(KUBECTL)" \
	  bash "$(ROOT_DIR)/scripts/check_url_ingest_freshness.sh"

health-integration-url-ingest:
	@echo "SKIP: url-ingest is a batch component; use run verification rather than a serving probe."

health-provider-k8s-mcp:
	@$(HELM) --namespace="$(DAEDALUS_NAMESPACE)" status k8s-mcp-server >/dev/null
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" rollout status deployment/k8s-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" wait --for=condition=Ready pod \
	  --selector=app.kubernetes.io/name=k8s-mcp-server,app.kubernetes.io/instance=k8s-mcp-server \
	  --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" exec deployment/k8s-mcp-server -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/livez >/dev/null

health-dependencies-k8s-mcp:
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" exec deployment/k8s-mcp-server -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/dependencyz >/dev/null

health-integration-k8s-mcp:
	$(call consumer_http,http://k8s-mcp-server.$(K8S_MCP_NAMESPACE).svc.cluster.local:8080/readyz)
	@$(KUBECTL) --namespace="$(AGENT_NAMESPACE)" exec -i "$(AGENT_WORKLOAD)" -- \
	  env MCP_URL="http://k8s-mcp-server.$(K8S_MCP_NAMESPACE).svc.cluster.local:8080/mcp" \
	    MCP_TOKEN_ENV=KUBERNETES_MCP_TOKEN python3 - < "$(ROOT_DIR)/scripts/check_mcp_contract.py"

health-provider-unifi-mcp:
	@$(HELM) --namespace="$(DAEDALUS_NAMESPACE)" status unifi-network-mcp-server >/dev/null
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" rollout status deployment/unifi-network-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" wait --for=condition=Ready pod \
	  --selector=app.kubernetes.io/name=unifi-network-mcp-server,app.kubernetes.io/instance=unifi-network-mcp-server \
	  --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" exec deployment/unifi-network-mcp-server -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/livez >/dev/null

health-dependencies-unifi-mcp:
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" exec deployment/unifi-network-mcp-server -- \
	  curl --fail --silent --show-error http://127.0.0.1:8080/dependencyz >/dev/null

health-integration-unifi-mcp:
	$(call consumer_http,http://unifi-network-mcp-server.$(UNIFI_MCP_NAMESPACE).svc.cluster.local:8080/readyz)
	@$(KUBECTL) --namespace="$(AGENT_NAMESPACE)" exec -i "$(AGENT_WORKLOAD)" -- \
	  env MCP_URL="http://unifi-network-mcp-server.$(UNIFI_MCP_NAMESPACE).svc.cluster.local:8080/mcp" \
	    MCP_TOKEN_ENV=UNIFI_MCP_TOKEN python3 - < "$(ROOT_DIR)/scripts/check_mcp_contract.py"

healthcheck-hindsight: health-provider-hindsight health-dependencies-hindsight $(if $(filter true,$(CHECK_CONSUMER)),health-integration-hindsight)
healthcheck-milvus: health-provider-milvus health-dependencies-milvus $(if $(filter true,$(CHECK_CONSUMER)),health-integration-milvus)
healthcheck-retriever: health-provider-retriever health-dependencies-retriever $(if $(filter true,$(CHECK_CONSUMER)),health-integration-retriever)
healthcheck-nv-ingest: health-provider-nv-ingest health-dependencies-nv-ingest $(if $(filter true,$(CHECK_CONSUMER)),health-integration-nv-ingest)
healthcheck-pdf-server: health-provider-pdf-server health-dependencies-pdf-server $(if $(filter true,$(CHECK_CONSUMER)),health-integration-pdf-server)
healthcheck-url-ingest: health-provider-url-ingest health-dependencies-url-ingest
healthcheck-k8s-mcp: health-provider-k8s-mcp health-dependencies-k8s-mcp $(if $(filter true,$(CHECK_CONSUMER)),health-integration-k8s-mcp)
healthcheck-unifi-mcp: health-provider-unifi-mcp health-dependencies-unifi-mcp $(if $(filter true,$(CHECK_CONSUMER)),health-integration-unifi-mcp)

##@ Data operations

publish-corpus: preflight ## [data write] Publish and atomically select a PDF corpus generation.
	@NAMESPACE="$(PDF_SERVER_NAMESPACE)" PDF_SOURCE_HOST_PATH="$(PDF_SOURCE_HOST_PATH)" \
	  PDF_SOURCE_NODE="$(PDF_SOURCE_NODE)" CORPUS_GENERATION="$(CORPUS_GENERATION)" \
	  bash "$(ROOT_DIR)/pdf-server/publish-corpus.sh"

stage-data: publish-corpus ## [data write] Publish PDFs; URL inputs are fetched by refresh workers.
	@echo 'PDF corpus published. URL sources are fetched conditionally by refresh-run.'

seed-data: publish-corpus ## [cluster/data write] Publish PDFs and submit refresh Jobs for COLLECTIONS. Does not wait.
	@$(MAKE) --no-print-directory refresh-run COLLECTIONS="$(COLLECTIONS)"

run-ingest: refresh-run ## [cluster/data write] Alias for refresh-run; submit Jobs for selected COLLECTIONS.

##@ Validation and integration

validate: validate-local validate-cluster ## [read-only cluster] Run local checks and cluster dry-runs.

validate-helm: ## [read-only local] Validate topology contracts and all eight Helm releases.
	@command -v python3 >/dev/null
	@command -v "$(HELM)" >/dev/null
	@"$(ROOT_DIR)/scripts/contextctl" validate --environment "$(ENVIRONMENT)"
	@"$(ROOT_DIR)/scripts/contextctl" render-agent-env --environment "$(ENVIRONMENT)" \
	  --check "$(GENERATED_AGENT_ENV)"
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
	  -s "$(ROOT_DIR)/scripts/tests" >/dev/null
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
	  -s "$(ROOT_DIR)/nemotron-retriever/tests" >/dev/null
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
	  -s "$(ROOT_DIR)/url-ingest/tests" -p 'test_refresh*.py' >/dev/null
	@$(MAKE) --no-print-directory -C "$(HINDSIGHT_ROOT)" validate >/dev/null
	@for chart in "$(MILVUS_CHART)" "$(RETRIEVER_CHART)" "$(NV_INGEST_CHART)" \
	  "$(PDF_SERVER_CHART)" "$(URL_INGEST_CHART)" "$(K8S_MCP_CHART)" "$(UNIFI_MCP_CHART)"; do \
	  $(HELM) lint "$$chart" --namespace "$(DAEDALUS_NAMESPACE)" --strict >/dev/null; \
	done
	@rendered="$$(mktemp)"; trap 'rm -f "$$rendered"' EXIT; \
	  $(HELM) template "$(MILVUS_RELEASE)" "$(MILVUS_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" --post-renderer "$(ROOT_DIR)/nim-milvus-ms/chart/post-render.py" >>"$$rendered"; \
	  $(HELM) template "$(RETRIEVER_RELEASE)" "$(RETRIEVER_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" \
	    --values "$(RETRIEVER_VALUES_FILE)" >>"$$rendered"; \
	  $(HELM) template "$(NV_INGEST_RELEASE)" "$(NV_INGEST_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" \
	    --post-renderer "$(ROOT_DIR)/nv-ingest/helm-post-renderer.sh" >>"$$rendered"; \
	  for chart in "$(PDF_SERVER_CHART)" "$(K8S_MCP_CHART)" "$(UNIFI_MCP_CHART)"; do \
	    $(HELM) template "$$(basename "$$chart")" "$$chart" --namespace "$(DAEDALUS_NAMESPACE)" >>"$$rendered"; \
	  done; \
	  $(HELM) template url-ingest "$(URL_INGEST_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" \
	    --set-file urlLists.kubernetes="$(ROOT_DIR)/url-ingest/files/k8s_url_links.txt" \
	    --set-file urlLists.vetpartner="$(ROOT_DIR)/url-ingest/files/vetpartner_url_links.txt" >>"$$rendered"; \
	  ! grep -Eq 'namespace: (milvus|nv-ingest|daedalus-retriever|k8s-mcp-server|unifi-network-mcp-server)$$' "$$rendered"
	@bash -n "$(ROOT_DIR)/scripts/check-shared-secrets.sh" \
	  "$(ROOT_DIR)/scripts/migrate-legacy-secrets.sh" \
	  "$(ROOT_DIR)/scripts/cleanup-legacy-stack.sh"
	@echo "Helm validation passed."

validate-local: validate-helm ## [read-only local] Run full source, image, and chart validation.
	@$(MAKE) --no-print-directory images-validate
	@bash -n \
	  "$(ROOT_DIR)/scripts/image-reference.sh" \
	  "$(ROOT_DIR)/scripts/check_url_ingest_freshness.sh" \
	  "$(ROOT_DIR)/pdf-server/publish-corpus.sh" \
	  "$(ROOT_DIR)/nv-ingest/helm-post-renderer.sh"
	@source "$(ROOT_DIR)/scripts/image-reference.sh"; \
	  require_semver_image_ref "validation image" "example.invalid/service:v1.2.3-rc.1"; \
	  ! require_semver_image_ref "validation image" "example.invalid/service:latest" >/dev/null 2>&1; \
	  ! require_semver_image_ref "validation image" \
	    "example.invalid/service@sha256:0000000000000000000000000000000000000000000000000000000000000000" \
	    >/dev/null 2>&1
	@PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile \
	  "$(ROOT_DIR)/scripts/check_cluster_capacity.py" \
	  "$(ROOT_DIR)/scripts/check_mcp_contract.py" \
	  "$(ROOT_DIR)/scripts/check_retriever_contract.py" \
	  "$(ROOT_DIR)/url-ingest/nv-ingest-ms-tasks.py"
	@cd "$(ROOT_DIR)/url-ingest" && python3 -m unittest discover -s tests >/dev/null
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s "$(ROOT_DIR)/nv-ingest/tests" >/dev/null
	@cd "$(ROOT_DIR)/pdf-server" && \
	  PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests >/dev/null
	@$(MAKE) --no-print-directory -C "$(ROOT_DIR)/k8s-multicluster-mcp" check >/dev/null
	@$(MAKE) --no-print-directory -C "$(ROOT_DIR)/unifi-network-mcp" test >/dev/null
	@$(MAKE) --no-print-directory -C "$(ROOT_DIR)/unifi-network-mcp" build >/dev/null
	@command -v docker >/dev/null || { \
	  echo "Docker is required for URL-ingest Rust validation; the host toolchain may not have a usable CRT." >&2; \
	  exit 1; \
	}
	@cd "$(ROOT_DIR)/url-ingest" && docker build --target check . >/dev/null
	@echo "Local validation passed."

validate-ha: ## [read-only local] Require an approved numeric HA acceptance policy.
	@"$(ROOT_DIR)/scripts/contextctl" validate --environment "$(ENVIRONMENT)" --require-ha

render-agent-env: ## [read-only local] Print generated Daedalus provider endpoints.
	@"$(ROOT_DIR)/scripts/contextctl" render-agent-env --environment "$(ENVIRONMENT)"

validate-cluster: preflight validate-helm ## [read-only cluster] Validate Helm manifests with Kubernetes client dry-runs.
	@KUBECTL="$(KUBECTL)" PYTHONDONTWRITEBYTECODE=1 \
	  python3 "$(ROOT_DIR)/scripts/check_cluster_capacity.py"
	@$(HELM) template "$(HINDSIGHT_RELEASE)" "$(HINDSIGHT_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" | \
	  $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" apply --dry-run=client -f - >/dev/null
	@$(HELM) template "$(MILVUS_RELEASE)" "$(MILVUS_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" --post-renderer "$(ROOT_DIR)/nim-milvus-ms/chart/post-render.py" | \
	  $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" apply --dry-run=client -f - >/dev/null
	@$(HELM) template "$(RETRIEVER_RELEASE)" "$(RETRIEVER_CHART)" \
	  --namespace "$(DAEDALUS_NAMESPACE)" --values "$(RETRIEVER_VALUES_FILE)" | \
	  $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" apply --dry-run=client -f - >/dev/null
	@$(HELM) template "$(NV_INGEST_RELEASE)" "$(NV_INGEST_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" \
	  --post-renderer "$(ROOT_DIR)/nv-ingest/helm-post-renderer.sh" | \
	  $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" apply --dry-run=client -f - >/dev/null
	@for chart in "$(PDF_SERVER_CHART)" "$(K8S_MCP_CHART)" "$(UNIFI_MCP_CHART)"; do \
	  $(HELM) template "$$(basename "$$chart")" "$$chart" --namespace "$(DAEDALUS_NAMESPACE)" | \
	    $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" apply --dry-run=client -f - >/dev/null; \
	done
	@$(HELM) template url-ingest "$(URL_INGEST_CHART)" --namespace "$(DAEDALUS_NAMESPACE)" \
	  --set-file urlLists.kubernetes="$(ROOT_DIR)/url-ingest/files/k8s_url_links.txt" \
	  --set-file urlLists.vetpartner="$(ROOT_DIR)/url-ingest/files/vetpartner_url_links.txt" | \
	  $(KUBECTL) --namespace="$(DAEDALUS_NAMESPACE)" apply --dry-run=client -f - >/dev/null
	@echo "Validation passed."
