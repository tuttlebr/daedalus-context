---
name: operate-daedalus-context
description: Operate and diagnose the Daedalus context Kubernetes stack. Use for root deployments, updates, restarts, image releases, health checks, ingestion and corpus jobs, Daedalus provider integration, or recovery in the daedalus-context repository. Do not use for isolated component code edits that do not affect stack operation.
---

# Operate Daedalus Context

Use the repository's root contracts and Make targets. Kubernetes is the only
runtime deployment path. Treat `docker-compose.yaml` only as the shared image
specification.

## Ground the Task

1. Read `README.md` and run `make help`.
2. Inspect `git status --short`. Preserve unrelated and component-local work.
3. Inspect `stack/topology.yaml`, the selected environment, and
   `stack/components.lock.yaml` before changing deployment behavior.
4. Determine whether the user requested diagnosis, source changes, a live
   operation, data mutation, or registry publication. Do not broaden one into
   another.

## Select the Root Workflow

- Validate or publish local images with `make images-validate`,
  `make images-build`, `make images-release-refs`, or `make images-publish`.
- Prepare a cluster operation with `make doctor`, `make show-config`, and
  `make validate-cluster`.
- Reconcile selected services with `make deploy` or `make update`. Use
  `make redeploy` only for an intentional restart.
- Diagnose in layers with `make health-provider`,
  `make health-dependencies`, and `make health-integration`.
- Use `make health-stack` for combined JSON. When exact exit codes matter, run
  `make preflight` and then call `scripts/contextctl health-stack` directly.
- Run `make publish-corpus`, `make stage-data`, `make seed-data`, or
  `make run-ingest` only when the user requested the corresponding data write.
- Generate Daedalus consumer settings with `make render-agent-env`. Keep
  secrets in the approved secret-management path.

Use `COMPONENTS` to limit scope. Include selected dependencies or prove that
existing dependencies are healthy before operating on a dependent alone.

## Enforce Safety Boundaries

- Source the root `.env` with export enabled. Never print or commit its secret
  values.
- Require an approved literal `EXPECTED_CONTEXT` for every Kubernetes command.
  Do not derive it from the active context in the same operation.
- Use the root Makefile for normal operations. Use child deployment wrappers
  only for focused render inspection, debugging, or reviewed recovery.
- Keep image build and registry publication separate from Kubernetes
  reconciliation.
- Never delete PVCs, reset etcd, drop collections, or rerun ingestion merely to
  test persistence or health.
- Treat a provider process check, dependency check, and Daedalus integration
  check as different evidence. Do not claim end-to-end health from one layer.
- For diagnosis requests, remain read-only. Perform a live write, registry
  push, or ingestion run only when the user authorizes that action.

## Validate and Hand Off

For source-only changes, run the narrow component tests and
`make validate-local`. For cluster-facing changes, also run
`make validate-cluster` with the approved context.

When a live operation is authorized, verify:

1. the orchestrator or Kubernetes control-plane result;
2. workload readiness and dependency health; and
3. a meaningful provider, ingestion, retrieval, or Daedalus data-plane path.

Report commands, selected components, side effects, and any deliberately
skipped live validation.
