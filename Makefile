SHELL := /usr/bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
comma := ,
KUBECTL ?= kubectl
HELM ?= helm
JQ ?= jq
COMPOSE ?= docker compose
ENV_FILE ?= $(ROOT_DIR)/.env
IMAGE_BUILD_SERVICES := k8s-mcp-server-read-only unifi-network-mcp-server pdf-server url-ingest nv-ingest-ms-tasks
DAEDALUS_CONTEXT_IMAGE_VERSION ?= 0.1.0
EXPECTED_CONTEXT ?=
ROLLOUT_TIMEOUT ?= 20m
ENVIRONMENT ?= production
GENERATED_AGENT_ENV ?= $(ROOT_DIR)/stack/generated/$(ENVIRONMENT)-daedalus-context.env
COMPOSE_FILE := $(ROOT_DIR)/docker-compose.yaml

# The agent consumes these services from this namespace. Services keep their
# existing namespaces by default so a routine deploy never creates new PVCs.
AGENT_NAMESPACE ?= daedalus
AGENT_WORKLOAD ?= deployment/daedalus-backend-default
STACK_NAMESPACE ?=

MILVUS_NAMESPACE ?= $(if $(strip $(STACK_NAMESPACE)),$(STACK_NAMESPACE),milvus)
NV_INGEST_NAMESPACE ?= $(if $(strip $(STACK_NAMESPACE)),$(STACK_NAMESPACE),nv-ingest)
PDF_SERVER_NAMESPACE ?= $(if $(strip $(STACK_NAMESPACE)),$(STACK_NAMESPACE),$(MILVUS_NAMESPACE))
URL_INGEST_NAMESPACE ?= $(if $(strip $(STACK_NAMESPACE)),$(STACK_NAMESPACE),$(MILVUS_NAMESPACE))
K8S_MCP_NAMESPACE ?= $(if $(strip $(STACK_NAMESPACE)),$(STACK_NAMESPACE),k8s-mcp-server)
UNIFI_MCP_NAMESPACE ?= $(if $(strip $(STACK_NAMESPACE)),$(STACK_NAMESPACE),unifi-network-mcp-server)
RETRIEVER_NAMESPACE ?= daedalus-retriever
K8S_MCP_NODE_PORT ?= 30080
UNIFI_MCP_NODE_PORT ?= 30081
MCP_EXTERNAL_CLIENT_CIDR ?= 192.168.1.0/24

MILVUS_RELEASE ?= milvus
NV_INGEST_RELEASE ?= nv-ingest
RETRIEVER_RELEASE ?= nemotron-retriever
RETRIEVER_FULLNAME ?= nemotron-retriever
RETRIEVER_CHART ?= $(ROOT_DIR)/nemotron-retriever
# Production-safe chart defaults are used unless the operator explicitly names
# an override. NodePort examples are never auto-discovered.
RETRIEVER_VALUES_FILE ?= nemotron-retriever/custom-values.yaml
RETRIEVER_ROLLOUT_TIMEOUT ?= 30m
MILVUS_SERVICE ?= $(MILVUS_RELEASE)
MINIO_SERVICE ?= $(MILVUS_RELEASE)-minio
NV_INGEST_SERVICE ?= $(NV_INGEST_RELEASE)
MILVUS_MINIO_SECRET_NAME ?= milvus-minio-credentials
MILVUS_ROOT_SECRET_NAME ?= milvus-root-credentials
# MinIO bulk data must not silently inherit the etcd-local class. Deployments
# must select a reviewed storage class explicitly.
MINIO_STORAGE_CLASS ?=
ETCD_STORAGE_CLASS ?= local-etcd
NV_INGEST_VERSION ?= 26.3.0

RETRIEVER_EMBEDDING_SERVICE ?= $(RETRIEVER_FULLNAME)-embedder
RETRIEVER_RERANKER_SERVICE ?= $(RETRIEVER_FULLNAME)-reranker
EMBEDDING_NIM_ENDPOINT ?= http://$(RETRIEVER_EMBEDDING_SERVICE).$(RETRIEVER_NAMESPACE).svc.cluster.local:8000/v1
EMBEDDING_NIM_MODEL_NAME ?= nvidia/llama-nemotron-embed-vl-1b-v2
DAEDALUS_RERANKER_ENDPOINT ?= http://$(RETRIEVER_RERANKER_SERVICE).$(RETRIEVER_NAMESPACE).svc.cluster.local:8000/rerank
NIM_RERANKER_ENDPOINT ?= http://$(RETRIEVER_RERANKER_SERVICE).$(RETRIEVER_NAMESPACE).svc.cluster.local:8000/v1/ranking
RERANKER_MODEL_NAME ?= nvidia/llama-nemotron-rerank-1b-v2

MILVUS_ENDPOINT ?= http://$(MILVUS_SERVICE).$(MILVUS_NAMESPACE).svc.cluster.local:19530
MINIO_ENDPOINT ?= http://$(MINIO_SERVICE).$(MILVUS_NAMESPACE).svc.cluster.local:9000
MINIO_INGEST_ENDPOINT ?= $(patsubst https://%,%,$(patsubst http://%,%,$(MINIO_ENDPOINT)))
NV_INGEST_ENDPOINT ?= http://$(NV_INGEST_SERVICE).$(NV_INGEST_NAMESPACE).svc.cluster.local:7670
NV_INGEST_MESSAGE_HOST ?= $(NV_INGEST_SERVICE).$(NV_INGEST_NAMESPACE).svc.cluster.local
NV_INGEST_MESSAGE_PORT ?= 7670
# Each replica reserves 24 CPU and 24 GiB. Two distinct nodes currently meet
# that placement contract; a third replica would remain Pending.
NV_INGEST_REPLICA_COUNT ?= 2
# Deliberately empty: production operators must select a shared, durable URI.
NV_INGEST_IMAGE_STORAGE_URI ?=
MINIO_BUCKET ?= nv-ingest
URL_INGEST_SOURCE_NODE ?= daedalus-01
NVIDIA_INGEST_SHARDS ?= 8
MAX_PARALLEL_INGEST_SHARDS ?= 1
INGEST_JOB_DEADLINE_SECONDS ?= 21600
INGEST_WAIT_TIMEOUT_SECONDS ?= 22200
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
UPDATE_BUILD_IMAGES ?= false
UPDATE_PUSH_IMAGES ?= false
EXPOSE_NODEPORTS ?= false
ENABLE_MILVUS_MONITORING ?= false
NV_INGEST_SERVICE_TYPE ?= ClusterIP
NV_INGEST_NODE_PORT ?=
# Leave empty for authenticated external NVCF endpoints: NV-Ingest's optional
# component readiness parser expects each dependency's unauthenticated
# /health/ready endpoint to return its native JSON schema. The ingestion
# preflight and real seed Jobs exercise those configured endpoints instead.
NV_INGEST_COMPONENTS_TO_READY_CHECK ?=
ALLOW_NV_INGEST_DEPLOY_DURING_INGEST ?= false
CHECK_CONSUMER ?= true
CONSUMER_RETRIES ?= 2

SUPPORTED_COMPONENTS := milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp
COMPONENTS ?= $(SUPPORTED_COMPONENTS)
UNKNOWN_COMPONENTS := $(filter-out $(SUPPORTED_COMPONENTS),$(COMPONENTS))

ifneq ($(strip $(UNKNOWN_COMPONENTS)),)
$(error Unsupported COMPONENTS: $(UNKNOWN_COMPONENTS). Choose from: $(SUPPORTED_COMPONENTS))
endif

.PHONY: help doctor show-config require-context \
	images-validate images-build images-push images-release-refs images-publish \
	compose-config compose-build compose-push compose-release-refs compose-publish \
	preflight validate validate-local validate-cluster validate-ha render-agent-env deploy update redeploy \
	healthcheck health-provider health-dependencies health-integration health-stack \
	healthcheck-agent publish-corpus stage-data seed-data run-ingest \
	deploy-milvus deploy-retriever deploy-nv-ingest deploy-pdf-server deploy-url-ingest deploy-k8s-mcp deploy-unifi-mcp \
	update-milvus update-retriever update-nv-ingest update-pdf-server update-url-ingest update-k8s-mcp update-unifi-mcp \
	redeploy-milvus redeploy-retriever redeploy-nv-ingest redeploy-pdf-server redeploy-url-ingest redeploy-k8s-mcp redeploy-unifi-mcp \
	health-provider-milvus health-provider-retriever health-provider-nv-ingest health-provider-pdf-server \
	health-provider-url-ingest health-provider-k8s-mcp health-provider-unifi-mcp \
	health-dependencies-milvus health-dependencies-retriever health-dependencies-nv-ingest \
	health-dependencies-pdf-server health-dependencies-url-ingest health-dependencies-k8s-mcp health-dependencies-unifi-mcp \
	health-integration-milvus health-integration-retriever health-integration-nv-ingest health-integration-pdf-server \
	health-integration-url-ingest health-integration-k8s-mcp health-integration-unifi-mcp \
	healthcheck-milvus healthcheck-retriever healthcheck-nv-ingest healthcheck-pdf-server healthcheck-url-ingest \
	healthcheck-k8s-mcp healthcheck-unifi-mcp

CLUSTER_TARGETS := preflight validate validate-cluster deploy update redeploy \
	health-provider health-dependencies health-integration health-stack healthcheck healthcheck-agent \
	publish-corpus stage-data seed-data run-ingest \
	deploy-milvus deploy-retriever deploy-nv-ingest deploy-pdf-server deploy-url-ingest deploy-k8s-mcp deploy-unifi-mcp \
	update-milvus update-retriever update-nv-ingest update-pdf-server update-url-ingest update-k8s-mcp update-unifi-mcp \
	redeploy-milvus redeploy-retriever redeploy-nv-ingest redeploy-pdf-server redeploy-url-ingest redeploy-k8s-mcp redeploy-unifi-mcp \
	health-provider-milvus health-provider-retriever health-provider-nv-ingest health-provider-pdf-server \
	health-provider-url-ingest health-provider-k8s-mcp health-provider-unifi-mcp \
	health-dependencies-milvus health-dependencies-retriever health-dependencies-nv-ingest \
	health-dependencies-pdf-server health-dependencies-url-ingest health-dependencies-k8s-mcp \
	health-dependencies-unifi-mcp health-integration-milvus health-integration-retriever \
	health-integration-nv-ingest health-integration-pdf-server health-integration-url-ingest \
	health-integration-k8s-mcp health-integration-unifi-mcp \
	healthcheck-milvus healthcheck-retriever healthcheck-nv-ingest healthcheck-pdf-server \
	healthcheck-url-ingest healthcheck-k8s-mcp healthcheck-unifi-mcp

$(CLUSTER_TARGETS): | require-context

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
	  'Use STACK_NAMESPACE only for a reviewed namespace and PVC migration.'

doctor: ## [read-only cluster] Check tools, exported configuration, context, and cluster access.
	@[[ -f "$(ENV_FILE)" ]] || { \
	  echo "Missing $(ENV_FILE). Copy .env.example, edit it, then source it with export enabled." >&2; exit 1; \
	}
	@bash -n "$(ENV_FILE)"
	@for command in bash python3 "$(JQ)" "$(KUBECTL)" "$(HELM)" docker; do \
	  command -v "$$command" >/dev/null || { echo "Missing required command: $$command" >&2; exit 1; }; \
	done
	@$(MAKE) --version | head -n 1 | grep -Fq 'GNU Make' || { echo 'GNU Make is required.' >&2; exit 1; }
	@$(COMPOSE) version >/dev/null
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
	  'nv-ingest version' '$(NV_INGEST_VERSION)' \
	  'nv-ingest namespace' '$(NV_INGEST_NAMESPACE)' \
	  'retriever namespace' '$(RETRIEVER_NAMESPACE)' \
	  'embedding model' '$(EMBEDDING_NIM_MODEL_NAME)' \
	  'reranker model' '$(RERANKER_MODEL_NAME)' \
	  'daedalus reranker API' '$(DAEDALUS_RERANKER_ENDPOINT)' \
	  'NIM reranker API' '$(NIM_RERANKER_ENDPOINT)' \
	  'NVIDIA ingest shards' '$(NVIDIA_INGEST_SHARDS)' \
	  'seed source node' '$(URL_INGEST_SOURCE_NODE)' \
	  'pdf-server namespace' '$(PDF_SERVER_NAMESPACE)' \
	  'url-ingest namespace' '$(URL_INGEST_NAMESPACE)' \
	  'k8s-mcp namespace' '$(K8S_MCP_NAMESPACE)' \
	  'k8s-mcp nodeport' '$(K8S_MCP_NODE_PORT)' \
	  'unifi-mcp namespace' '$(UNIFI_MCP_NAMESPACE)' \
	  'unifi-mcp nodeport' '$(UNIFI_MCP_NODE_PORT)' \
	  'MCP external clients' '$(MCP_EXTERNAL_CLIENT_CIDR)' \
	  'expected context' '$(or $(EXPECTED_CONTEXT),<required>)'

##@ Image lifecycle

images-validate: ## [read-only local] Validate all five versioned image specifications.
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
	resolve_image URL_INGEST_RUNNER_IMAGE nv-ingest-ms-tasks

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

deploy: preflight ## [cluster write] Reconcile selected components without forcing restarts.
	@"$(ROOT_DIR)/scripts/contextctl" operate --action deploy --components $(COMPONENTS)

update: preflight ## [cluster write] Reconcile selected already-published release images.
	@"$(ROOT_DIR)/scripts/contextctl" operate --action update --components $(COMPONENTS)

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
	  "$${values_args[@]}" --timeout "$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	for deployment in "$(RETRIEVER_FULLNAME)-embedder" "$(RETRIEVER_FULLNAME)-reranker"; do \
	  $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" rollout status "deployment/$$deployment" \
	    --timeout="$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	done; \
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
	@cd "$(ROOT_DIR)/nim-milvus-ms" && \
	  NAMESPACE="$(MILVUS_NAMESPACE)" RELEASE_NAME="$(MILVUS_RELEASE)" \
	  MINIO_SECRET_NAME="$(MILVUS_MINIO_SECRET_NAME)" \
	  MILVUS_ROOT_SECRET_NAME="$(MILVUS_ROOT_SECRET_NAME)" \
	  $(if $(strip $(MINIO_STORAGE_CLASS)),MINIO_STORAGE_CLASS="$(MINIO_STORAGE_CLASS)",) \
	  ETCD_STORAGE_CLASS="$(ETCD_STORAGE_CLASS)" \
	  EXPOSE_NODEPORTS="$(EXPOSE_NODEPORTS)" ENABLE_MONITORING="$(ENABLE_MILVUS_MONITORING)" \
	  HELM_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

deploy-nv-ingest:
	@cd "$(ROOT_DIR)/nv-ingest" && \
	  NAMESPACE="$(NV_INGEST_NAMESPACE)" RELEASE_NAME="$(NV_INGEST_RELEASE)" \
	  MILVUS_NAMESPACE="$(MILVUS_NAMESPACE)" \
	  HELM_TIMEOUT="$(ROLLOUT_TIMEOUT)" \
	  NV_INGEST_CHART_URL="https://helm.ngc.nvidia.com/nvidia/nemo-microservices/charts/nv-ingest-$(NV_INGEST_VERSION).tgz" \
	  NV_INGEST_IMAGE_TAG="$(NV_INGEST_VERSION)" \
	  MILVUS_ENDPOINT="$(MILVUS_ENDPOINT)" \
	  MINIO_INTERNAL_ADDRESS="$(MINIO_ENDPOINT)" MINIO_PUBLIC_ADDRESS="$(MINIO_ENDPOINT)" \
	  MINIO_BUCKET="$(MINIO_BUCKET)" MINIO_SOURCE_SECRET_NAMESPACE="$(MILVUS_NAMESPACE)" \
	  MINIO_SOURCE_SECRET_NAME="$(MILVUS_MINIO_SECRET_NAME)" \
	  EMBEDDING_NIM_ENDPOINT="$(EMBEDDING_NIM_ENDPOINT)" \
	  EMBEDDING_NIM_MODEL_NAME="$(EMBEDDING_NIM_MODEL_NAME)" \
	  NV_INGEST_REPLICA_COUNT="$(NV_INGEST_REPLICA_COUNT)" \
	  $(if $(strip $(NV_INGEST_IMAGE_STORAGE_URI)),IMAGE_STORAGE_URI="$(NV_INGEST_IMAGE_STORAGE_URI)",) \
	  URL_INGEST_NAMESPACE="$(URL_INGEST_NAMESPACE)" \
	  ALLOW_DEPLOY_DURING_INGEST="$(ALLOW_NV_INGEST_DEPLOY_DURING_INGEST)" \
	  NV_INGEST_SERVICE_TYPE="$(NV_INGEST_SERVICE_TYPE)" NV_INGEST_NODE_PORT="$(NV_INGEST_NODE_PORT)" \
	  COMPONENTS_TO_READY_CHECK="$(NV_INGEST_COMPONENTS_TO_READY_CHECK)" bash deploy.sh

deploy-pdf-server:
	@cd "$(ROOT_DIR)/pdf-server" && \
	  TARGET=kubernetes NAMESPACE="$(PDF_SERVER_NAMESPACE)" \
	  AGENT_NAMESPACE="$(AGENT_NAMESPACE)" URL_INGEST_NAMESPACE="$(URL_INGEST_NAMESPACE)" \
	  PDF_SERVER_IMAGE="$(PDF_SERVER_IMAGE)" PDF_STORAGE_CLASS="$(PDF_STORAGE_CLASS)" \
	  PDF_STORAGE_SIZE="$(PDF_STORAGE_SIZE)" \
	  ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

deploy-url-ingest:
	@cd "$(ROOT_DIR)/url-ingest" && \
	  NAMESPACE="$(URL_INGEST_NAMESPACE)" MILVUS_NAMESPACE="$(MILVUS_NAMESPACE)" \
	  NV_INGEST_NAMESPACE="$(NV_INGEST_NAMESPACE)" ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" \
	  MILVUS_SERVICE="$(MILVUS_SERVICE)" MINIO_SERVICE="$(MINIO_SERVICE)" \
	  NV_INGEST_SERVICE="$(NV_INGEST_SERVICE)" MILVUS_URI="$(MILVUS_ENDPOINT)" \
	  MINIO_ENDPOINT="$(MINIO_INGEST_ENDPOINT)" MINIO_BUCKET="$(MINIO_BUCKET)" \
	  NV_INGEST_MESSAGE_HOST="$(NV_INGEST_MESSAGE_HOST)" NV_INGEST_MESSAGE_PORT="$(NV_INGEST_MESSAGE_PORT)" \
	  NV_INGEST_VERSION="$(NV_INGEST_VERSION)" MINIO_SOURCE_SECRET_NAMESPACE="$(MILVUS_NAMESPACE)" \
	  MINIO_SOURCE_SECRET_NAME="$(MILVUS_MINIO_SECRET_NAME)" \
	  MILVUS_ROOT_SECRET_NAME="$(MILVUS_ROOT_SECRET_NAME)" URL_INGEST_SOURCE_NODE="$(URL_INGEST_SOURCE_NODE)" \
	  bash k8s/deploy.sh

deploy-k8s-mcp:
	@cd "$(ROOT_DIR)/k8s-multicluster-mcp" && \
	  K8S_MCP_NAMESPACE="$(K8S_MCP_NAMESPACE)" BUILD_IMAGE=false PUSH_IMAGE=false \
	  K8S_MCP_NODE_PORT="$(K8S_MCP_NODE_PORT)" \
	  MCP_EXTERNAL_CLIENT_CIDR="$(MCP_EXTERNAL_CLIENT_CIDR)" \
	  ROLLOUT_RESTART=false ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

deploy-unifi-mcp:
	@cd "$(ROOT_DIR)/unifi-network-mcp" && \
	  UNIFI_MCP_NAMESPACE="$(UNIFI_MCP_NAMESPACE)" BUILD_IMAGE=false PUSH_IMAGE=false \
	  UNIFI_MCP_NODE_PORT="$(UNIFI_MCP_NODE_PORT)" \
	  MCP_EXTERNAL_CLIENT_CIDR="$(MCP_EXTERNAL_CLIENT_CIDR)" \
	  ROLLOUT_RESTART=false ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

# Updates --------------------------------------------------------------------

update-milvus: deploy-milvus
update-retriever: deploy-retriever
update-nv-ingest: deploy-nv-ingest
update-url-ingest: deploy-url-ingest

update-pdf-server: deploy-pdf-server
	@$(MAKE) --no-print-directory redeploy-pdf-server

update-k8s-mcp:
	@cd "$(ROOT_DIR)/k8s-multicluster-mcp" && \
	  K8S_MCP_NAMESPACE="$(K8S_MCP_NAMESPACE)" \
	  BUILD_IMAGE="$(UPDATE_BUILD_IMAGES)" PUSH_IMAGE="$(UPDATE_PUSH_IMAGES)" \
	  ROLLOUT_RESTART=false ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

update-unifi-mcp:
	@cd "$(ROOT_DIR)/unifi-network-mcp" && \
	  UNIFI_MCP_NAMESPACE="$(UNIFI_MCP_NAMESPACE)" \
	  BUILD_IMAGE="$(UPDATE_BUILD_IMAGES)" PUSH_IMAGE="$(UPDATE_PUSH_IMAGES)" \
	  ROLLOUT_RESTART=false ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

# Restarts -------------------------------------------------------------------

redeploy-milvus:
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout restart statefulset --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status statefulset --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout restart statefulset --selector="release=$(MILVUS_RELEASE)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status statefulset --selector="release=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout restart deployment --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status deployment --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"

redeploy-retriever:
	@for deployment in "$(RETRIEVER_FULLNAME)-embedder" "$(RETRIEVER_FULLNAME)-reranker"; do \
	  $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" get deployment "$$deployment" >/dev/null; \
	  before="$$($(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" get deployment "$$deployment" \
	    -o jsonpath='{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}')"; \
	  $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" rollout restart "deployment/$$deployment"; \
	  $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" rollout status "deployment/$$deployment" \
	    --timeout="$(RETRIEVER_ROLLOUT_TIMEOUT)"; \
	  after="$$($(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" get deployment "$$deployment" \
	    -o jsonpath='{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}')"; \
	  [[ -n "$$after" && "$$after" != "$$before" ]] || { \
	    echo "Restart annotation did not change for deployment/$$deployment." >&2; exit 1; \
	  }; \
	done

redeploy-nv-ingest:
	@$(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" rollout restart statefulset --selector="app.kubernetes.io/instance=$(NV_INGEST_RELEASE)"
	@$(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" rollout status statefulset --selector="app.kubernetes.io/instance=$(NV_INGEST_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" rollout restart deployment --selector="app.kubernetes.io/instance=$(NV_INGEST_RELEASE)"
	@$(KUBECTL) --namespace="$(NV_INGEST_NAMESPACE)" rollout status deployment --selector="app.kubernetes.io/instance=$(NV_INGEST_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"

redeploy-pdf-server:
	@$(KUBECTL) --namespace="$(PDF_SERVER_NAMESPACE)" rollout restart deployment/pdf-server
	@$(KUBECTL) --namespace="$(PDF_SERVER_NAMESPACE)" rollout status deployment/pdf-server --timeout="$(ROLLOUT_TIMEOUT)"

redeploy-url-ingest:
	@echo "CronJobs have no persistent pods; reconciling their job templates instead."
	@$(MAKE) --no-print-directory deploy-url-ingest

redeploy-k8s-mcp:
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" rollout restart deployment/k8s-mcp-server
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" rollout status deployment/k8s-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"

redeploy-unifi-mcp:
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" rollout restart deployment/unifi-network-mcp-server
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" rollout status deployment/unifi-network-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"

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

health-provider-milvus:
	@$(HELM) --namespace="$(MILVUS_NAMESPACE)" status "$(MILVUS_RELEASE)" >/dev/null
	$(call assert_nonempty_resources,$(MILVUS_NAMESPACE),deployment,app.kubernetes.io/instance=$(MILVUS_RELEASE))
	$(call assert_nonempty_resources,$(MILVUS_NAMESPACE),statefulset,app.kubernetes.io/instance=$(MILVUS_RELEASE))
	$(call assert_nonempty_resources,$(MILVUS_NAMESPACE),statefulset,release=$(MILVUS_RELEASE))
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status deployment --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status statefulset --selector="app.kubernetes.io/instance=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(MILVUS_NAMESPACE)" rollout status statefulset --selector="release=$(MILVUS_RELEASE)" --timeout="$(ROLLOUT_TIMEOUT)"
	$(call assert_bound_pvcs,$(MILVUS_NAMESPACE),daedalus.nvidia.com/milvus-storage=true$(comma)app.kubernetes.io/instance=$(MILVUS_RELEASE))

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

health-integration-nv-ingest:
	$(call consumer_http,$(NV_INGEST_ENDPOINT)/v1/health/ready)

health-provider-pdf-server:
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
	$(call assert_bound_pvcs,$(URL_INGEST_NAMESPACE),app.kubernetes.io/name=url-ingest)
	$(call assert_resource_count,$(URL_INGEST_NAMESPACE),cronjob,app.kubernetes.io/name=url-ingest,5)

health-dependencies-url-ingest:
	@NAMESPACE="$(URL_INGEST_NAMESPACE)" COLLECTIONS="$(REQUIRED_COLLECTIONS)" \
	  RUNNING_GRACE_SECONDS="$(URL_INGEST_RUNNING_GRACE_SECONDS)" KUBECTL="$(KUBECTL)" \
	  bash "$(ROOT_DIR)/scripts/check_url_ingest_freshness.sh"

health-integration-url-ingest:
	@echo "SKIP: url-ingest is a batch component; use run verification rather than a serving probe."

health-provider-k8s-mcp:
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" rollout status deployment/k8s-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(K8S_MCP_NAMESPACE)" wait --for=condition=Ready pod --selector=app=k8s-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"
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
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" rollout status deployment/unifi-network-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"
	@$(KUBECTL) --namespace="$(UNIFI_MCP_NAMESPACE)" wait --for=condition=Ready pod --selector=app=unifi-network-mcp-server --timeout="$(ROLLOUT_TIMEOUT)"
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

healthcheck-milvus: health-provider-milvus health-dependencies-milvus $(if $(filter true,$(CHECK_CONSUMER)),health-integration-milvus)
healthcheck-retriever: health-provider-retriever health-dependencies-retriever $(if $(filter true,$(CHECK_CONSUMER)),health-integration-retriever)
healthcheck-nv-ingest: health-provider-nv-ingest health-dependencies-nv-ingest $(if $(filter true,$(CHECK_CONSUMER)),health-integration-nv-ingest)
healthcheck-pdf-server: health-provider-pdf-server health-dependencies-pdf-server $(if $(filter true,$(CHECK_CONSUMER)),health-integration-pdf-server)
healthcheck-url-ingest: health-provider-url-ingest health-dependencies-url-ingest
healthcheck-k8s-mcp: health-provider-k8s-mcp health-dependencies-k8s-mcp $(if $(filter true,$(CHECK_CONSUMER)),health-integration-k8s-mcp)
healthcheck-unifi-mcp: health-provider-unifi-mcp health-dependencies-unifi-mcp $(if $(filter true,$(CHECK_CONSUMER)),health-integration-unifi-mcp)

##@ Data operations

publish-corpus: preflight ## [data write] Publish and atomically select a PDF corpus generation.
	@cd "$(ROOT_DIR)/pdf-server" && \
	  TARGET=kubernetes NAMESPACE="$(PDF_SERVER_NAMESPACE)" PUBLISH_CORPUS=true \
	  AGENT_NAMESPACE="$(AGENT_NAMESPACE)" URL_INGEST_NAMESPACE="$(URL_INGEST_NAMESPACE)" \
	  PDF_SERVER_IMAGE="$(PDF_SERVER_IMAGE)" PDF_STORAGE_CLASS="$(PDF_STORAGE_CLASS)" \
	  PDF_STORAGE_SIZE="$(PDF_STORAGE_SIZE)" PDF_SOURCE_HOST_PATH="$(PDF_SOURCE_HOST_PATH)" \
	  PDF_SOURCE_NODE="$(PDF_SOURCE_NODE)" CORPUS_GENERATION="$(CORPUS_GENERATION)" \
	  ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" bash deploy.sh

define run_ingest_operation
	@cd "$(ROOT_DIR)/url-ingest" && \
	  NAMESPACE="$(URL_INGEST_NAMESPACE)" MILVUS_NAMESPACE="$(MILVUS_NAMESPACE)" \
	  NV_INGEST_NAMESPACE="$(NV_INGEST_NAMESPACE)" ROLLOUT_TIMEOUT="$(ROLLOUT_TIMEOUT)" \
	  MILVUS_SERVICE="$(MILVUS_SERVICE)" MINIO_SERVICE="$(MINIO_SERVICE)" \
	  NV_INGEST_SERVICE="$(NV_INGEST_SERVICE)" MILVUS_URI="$(MILVUS_ENDPOINT)" \
	  MINIO_ENDPOINT="$(MINIO_INGEST_ENDPOINT)" MINIO_BUCKET="$(MINIO_BUCKET)" \
	  NV_INGEST_MESSAGE_HOST="$(NV_INGEST_MESSAGE_HOST)" NV_INGEST_MESSAGE_PORT="$(NV_INGEST_MESSAGE_PORT)" \
	  NV_INGEST_VERSION="$(NV_INGEST_VERSION)" MINIO_SOURCE_SECRET_NAMESPACE="$(MILVUS_NAMESPACE)" \
	  MINIO_SOURCE_SECRET_NAME="$(MILVUS_MINIO_SECRET_NAME)" \
	  MILVUS_ROOT_SECRET_NAME="$(MILVUS_ROOT_SECRET_NAME)" URL_INGEST_SOURCE_NODE="$(URL_INGEST_SOURCE_NODE)" \
	  NVIDIA_INGEST_SHARDS="$(NVIDIA_INGEST_SHARDS)" \
	  MAX_PARALLEL_INGEST_SHARDS="$(MAX_PARALLEL_INGEST_SHARDS)" \
	  INGEST_JOB_DEADLINE_SECONDS="$(INGEST_JOB_DEADLINE_SECONDS)" \
	  INGEST_WAIT_TIMEOUT_SECONDS="$(INGEST_WAIT_TIMEOUT_SECONDS)" \
	  bash k8s/deploy.sh $(1)
endef

stage-data: publish-corpus ## [data write] Publish PDFs and stage URL inputs without ingesting them.
	$(call run_ingest_operation,--stage-data)

seed-data: publish-corpus ## [data write] Publish, stage, ingest, wait, and verify collections.
	$(call run_ingest_operation,--stage-data --run-now $(COLLECTIONS))

run-ingest: preflight ## [data write] Ingest staged data, wait, and verify selected collections.
	$(call run_ingest_operation,--run-now $(COLLECTIONS))

##@ Validation and integration

validate: validate-local validate-cluster ## [read-only cluster] Run local checks and cluster dry-runs.

validate-local: ## [read-only local] Run source tests and renders without cluster credentials.
	@command -v python3 >/dev/null
	@command -v "$(HELM)" >/dev/null
	@$(MAKE) --no-print-directory images-validate
	@"$(ROOT_DIR)/scripts/contextctl" validate --environment "$(ENVIRONMENT)"
	@"$(ROOT_DIR)/scripts/contextctl" render-agent-env --environment "$(ENVIRONMENT)" \
	  --check "$(GENERATED_AGENT_ENV)"
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
	  -s "$(ROOT_DIR)/scripts/tests" >/dev/null
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
	  -s "$(RETRIEVER_CHART)/tests" >/dev/null
	@$(HELM) lint "$(RETRIEVER_CHART)" --namespace "$(RETRIEVER_NAMESPACE)" --strict >/dev/null
	@$(HELM) template "$(RETRIEVER_RELEASE)" "$(RETRIEVER_CHART)" \
	  --namespace validation-nondefault \
	  --set-string fullnameOverride="$(RETRIEVER_FULLNAME)" >/dev/null
	@bash -n \
	  "$(ROOT_DIR)/scripts/image-reference.sh" \
	  "$(ROOT_DIR)/scripts/check_url_ingest_freshness.sh" \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy.sh" \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy.sh" \
	  "$(ROOT_DIR)/nim-milvus-ms/deploy.sh" \
	  "$(ROOT_DIR)/nv-ingest/deploy.sh" \
	  "$(ROOT_DIR)/pdf-server/deploy.sh" \
	  "$(ROOT_DIR)/url-ingest/k8s/deploy.sh" \
	  "$(ROOT_DIR)/url-ingest/k8s/lib/job-control.sh"
	@grep -Fq 'type: LoadBalancer' \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy/k8s-mcp-server.yaml"
	@grep -Fq 'loadBalancerClass: daedalus.ai/nodeport-only' \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy/k8s-mcp-server.yaml"
	@grep -Fq 'nodePort: __K8S_MCP_NODE_PORT__' \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy/k8s-mcp-server.yaml"
	@grep -Fq 'loadBalancerSourceRanges:' \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy/k8s-mcp-server.yaml"
	@grep -Fq -- '- __MCP_EXTERNAL_CLIENT_CIDR__' \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy/k8s-mcp-server.yaml"
	@grep -Fq 'type: LoadBalancer' \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy/unifi-network-mcp-server.yaml"
	@grep -Fq 'loadBalancerClass: daedalus.ai/nodeport-only' \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy/unifi-network-mcp-server.yaml"
	@grep -Fq 'nodePort: __UNIFI_MCP_NODE_PORT__' \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy/unifi-network-mcp-server.yaml"
	@grep -Fq 'loadBalancerSourceRanges:' \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy/unifi-network-mcp-server.yaml"
	@grep -Fq -- '- __MCP_EXTERNAL_CLIENT_CIDR__' \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy/unifi-network-mcp-server.yaml"
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
	  "$(ROOT_DIR)/url-ingest/nv-ingest-ms-tasks.py" \
	  "$(ROOT_DIR)/url-ingest/k8s/normalize-manifest.py" \
	  "$(ROOT_DIR)/url-ingest/k8s/render-manual-job.py"
	@cd "$(ROOT_DIR)/url-ingest" && python3 -m unittest discover -s tests >/dev/null
	@cd "$(ROOT_DIR)/nv-ingest" && bash tests/test-deploy.sh >/dev/null
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

validate-cluster: preflight validate-local ## [read-only cluster] Validate active manifests with dry-runs.
	@KUBECTL="$(KUBECTL)" PYTHONDONTWRITEBYTECODE=1 \
	  python3 "$(ROOT_DIR)/scripts/check_cluster_capacity.py"
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
	  -s "$(RETRIEVER_CHART)/tests" >/dev/null
	@$(HELM) lint "$(RETRIEVER_CHART)" --namespace "$(RETRIEVER_NAMESPACE)" --strict >/dev/null
	@$(HELM) template "$(RETRIEVER_RELEASE)" "$(RETRIEVER_CHART)" \
	  --namespace "$(RETRIEVER_NAMESPACE)" \
	  --set-string fullnameOverride="$(RETRIEVER_FULLNAME)" | \
	  $(KUBECTL) --namespace="$(RETRIEVER_NAMESPACE)" apply --dry-run=client -f - >/dev/null
	@bash -n \
	  "$(ROOT_DIR)/scripts/image-reference.sh" \
	  "$(ROOT_DIR)/k8s-multicluster-mcp/deploy.sh" \
	  "$(ROOT_DIR)/unifi-network-mcp/deploy.sh" \
	  "$(ROOT_DIR)/nim-milvus-ms/deploy.sh" \
	  "$(ROOT_DIR)/nv-ingest/deploy.sh" \
	  "$(ROOT_DIR)/pdf-server/deploy.sh" \
	  "$(ROOT_DIR)/url-ingest/k8s/deploy.sh" \
	  "$(ROOT_DIR)/url-ingest/k8s/lib/job-control.sh"
	@python3 -m py_compile \
	  "$(ROOT_DIR)/url-ingest/nv-ingest-ms-tasks.py" \
	  "$(ROOT_DIR)/url-ingest/k8s/render-manual-job.py"
	@cd "$(ROOT_DIR)/url-ingest" && python3 -m unittest discover -s tests >/dev/null
	@cd "$(ROOT_DIR)/nv-ingest" && bash tests/test-deploy.sh >/dev/null
	@cd "$(ROOT_DIR)/nv-ingest" && \
	  set -a; \
	  [[ ! -f "$(ROOT_DIR)/.env" ]] || source "$(ROOT_DIR)/.env"; \
	  [[ ! -f .env ]] || source .env; \
	  set +a; \
	  bash validate.sh >/dev/null
	@MCP_AUTH_TOKEN=validation \
	  K8S_MCP_IMAGE="example.invalid/k8s-mcp:0.0.0" \
	  DRY_RUN=true BUILD_IMAGE=false PUSH_IMAGE=false \
	  K8S_MCP_NAMESPACE="$(K8S_MCP_NAMESPACE)" bash "$(ROOT_DIR)/k8s-multicluster-mcp/deploy.sh" >/dev/null
	@MCP_AUTH_TOKEN=validation \
	  UNIFI_MCP_IMAGE="example.invalid/unifi-mcp:0.0.0" \
	  DRY_RUN=true BUILD_IMAGE=false PUSH_IMAGE=false \
	  UNIFI_MCP_NAMESPACE="$(UNIFI_MCP_NAMESPACE)" bash "$(ROOT_DIR)/unifi-network-mcp/deploy.sh" >/dev/null
	@TARGET=kubernetes DRY_RUN=true NAMESPACE="$(PDF_SERVER_NAMESPACE)" \
	  bash "$(ROOT_DIR)/pdf-server/deploy.sh" >/dev/null
	@for manifest in serviceaccount.yaml pvc.yaml configmap-url-lists.yaml \
	  cronjob-nvidia.yaml cronjob-kubernetes.yaml cronjob-vetpartner.yaml \
	  cronjob-semianalysis.yaml cronjob-mentalhealth.yaml seed-data-job.yaml; do \
	  sed "s/^\([[:space:]]*\)namespace: milvus$$/\1namespace: $(URL_INGEST_NAMESPACE)/" \
	    "$(ROOT_DIR)/url-ingest/k8s/$$manifest" | \
	    sed -e 's/__SOURCE_NODE__/$(URL_INGEST_SOURCE_NODE)/g' \
	      -e 's/__PRUNE_STAGED_DATA__/true/g' | \
	    $(KUBECTL) --namespace="$(URL_INGEST_NAMESPACE)" apply --dry-run=client -f - >/dev/null; \
	done
	@cd "$(ROOT_DIR)/url-ingest" && \
	  rendered="$$(NAMESPACE="$(URL_INGEST_NAMESPACE)" MILVUS_NAMESPACE="$(MILVUS_NAMESPACE)" \
	    NV_INGEST_NAMESPACE="$(NV_INGEST_NAMESPACE)" MILVUS_SERVICE="$(MILVUS_SERVICE)" \
	    MINIO_SERVICE="$(MINIO_SERVICE)" NV_INGEST_SERVICE="$(NV_INGEST_SERVICE)" \
	    MILVUS_URI="$(MILVUS_ENDPOINT)" MINIO_ENDPOINT="$(MINIO_INGEST_ENDPOINT)" \
	    MINIO_BUCKET="$(MINIO_BUCKET)" NV_INGEST_MESSAGE_HOST="$(NV_INGEST_MESSAGE_HOST)" \
	    NV_INGEST_VERSION="$(NV_INGEST_VERSION)" URL_INGEST_SOURCE_NODE="$(URL_INGEST_SOURCE_NODE)" \
	    URL_INGEST_IMAGE="example.invalid/url-ingest:0.0.0" \
	    URL_INGEST_RUNNER_IMAGE="example.invalid/url-ingest-runner:0.0.0" \
	    bash k8s/deploy.sh --render-only)"; \
	  grep -q 'image: example.invalid/url-ingest:0.0.0' <<<"$$rendered"; \
	  grep -q 'image: example.invalid/url-ingest-runner:0.0.0' <<<"$$rendered"; \
	  ! grep -q '__URL_INGEST_IMAGE__' <<<"$$rendered"; \
	  ! grep -q '__URL_INGEST_RUNNER_IMAGE__' <<<"$$rendered"; \
	  grep -Fq 'value: $(MINIO_BUCKET)' <<<"$$rendered"; \
	  [[ "$$(grep -c '^  suspend: false$$' <<<"$$rendered")" -eq 5 ]]; \
	  printf '%s\n' "$$rendered" | $(KUBECTL) --namespace="$(URL_INGEST_NAMESPACE)" \
	    apply --dry-run=client -f - >/dev/null
	@cd "$(ROOT_DIR)/nim-milvus-ms" && NAMESPACE="$(MILVUS_NAMESPACE)" bash validate.sh >/dev/null
	@echo "Validation passed."
