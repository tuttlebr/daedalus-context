---
name: operate-daedalus-context
description: Operate and diagnose the Daedalus context Kubernetes stack, including releases, recovery, continuous refresh, and Daedalus integration. Use for root operational workflows and changes to their contracts; keep this guidance aligned with verified repository changes. Excludes isolated component edits with no operational impact.
---

# Operate Daedalus Context

Use the repository's root contracts and Make targets. Kubernetes is the only
runtime deployment path; `docker-compose.yaml` defines images for validation,
building, and publishing.

## Ground the Task

1. Read the relevant sections of [README.md](../../../README.md) and run
   `make help` from the repository root. Check the current
   [Makefile](../../../Makefile) for command arguments and side effects.
2. Inspect `git status --short`. Preserve unrelated and component-local work.
3. Before changing deployment behavior, inspect
   [topology](../../../stack/topology.yaml), the selected file in
   [environments](../../../stack/environments/), and the
   [component lock](../../../stack/components.lock.yaml). Read applicable
   component `AGENTS.md` files before editing their implementation.
4. Determine whether the user requested diagnosis, source changes, a live
   operation, data mutation, or registry publication, including authorization
   already given in this conversation. Keep actions within that scope.

Repository contracts and tested implementation determine current behavior. If
this skill disagrees, verify the discrepancy and correct the stale guidance as
part of the task; do not adapt the runtime to an outdated instruction.

## Select the Root Workflow

- Validate or publish local images with `make images-validate`,
  `make images-build`, `make images-release-refs`, or `make images-publish`.
- Prepare a cluster operation with `make doctor`, `make show-config`, and
  `make validate-cluster`.
- Reconcile selected services with `make deploy` or `make update`. Use
  `make redeploy` only for an intentional restart.
- Diagnose in layers with `make health-provider`,
  `make health-dependencies`, and `make health-integration`.
- Inspect restart blockers with `make recovery-status`; use `make restart`
  for an authorized restart with existing storage.
- Use `make health-stack` for combined JSON. When exact exit codes matter, run
  `make preflight` and then call `scripts/contextctl health-stack` directly.
- For refresh failures, stale collections, ingestion changes, or embedding
  migrations, read [Refresh operations](references/refresh-operations.md).
  `make refresh-status` reads durable status; `make refresh-run` writes data;
  `make refresh-verify` creates a Kubernetes Job that reads retrieval data.
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
- Preserve working LAN endpoints and consumer connections. Before changing
  exposure, inventory the actual clients, ports, routes, and allowlists; retain
  rollback configuration. Obtain approval for a compatibility break. Verify
  affected API paths from an ordinary non-node LAN client, or report an explicit
  user-approved skip. Cluster-only probes do not establish LAN compatibility.
- Use the root Makefile for normal operations. Use child deployment wrappers
  only for focused render inspection, debugging, or reviewed recovery.
- Keep image build and registry publication separate from Kubernetes
  reconciliation.
- Never delete PVCs, reset etcd, drop collections, or rerun ingestion merely to
  test persistence or health.
- Treat a provider process check, dependency check, and Daedalus integration
  check as different evidence. Do not claim end-to-end health from one layer.
- For diagnosis requests, remain read-only unless remediation is already
  authorized. A read-only data probe may still create a cluster resource;
  classify both effects before running it.

## Validate and Hand Off

For executable source changes, run the relevant component tests and
`make validate-local`. For cluster-facing changes, also run
`make validate-cluster` with the approved context. For documentation or skill
edits alone, validate the changed links, command references, and skill metadata;
building images or accessing the cluster is unnecessary.

When a live operation is authorized, verify:

1. the orchestrator or Kubernetes control-plane result;
2. workload readiness and dependency health; and
3. a meaningful provider, ingestion, retrieval, or Daedalus data-plane path.

Report commands, selected components, side effects, and any deliberately
skipped live validation. Distinguish recovered service, remaining source
failures, and background work still awaiting acceptance.

## Keep the Skill Current

Before handing off a task that changes root workflows, contracts, or recovery
behavior, review the relevant diff and verified findings for a skill update.
Also review after a reusable user correction, a demonstrated recovery gap, or
a stale command or link. This is part of completing the task when the skill is
writable, not a separate deployment or scheduled background process.

- Make the smallest supported correction. Update workflow selection and safety
  invariants here; keep conditional operational detail in linked references.
- Keep versions, endpoints, schedules, timeout values, and model settings in
  their canonical repository files. Link to those files instead of copying
  values into the skill. Use code, tests, or verified runtime evidence to support
  each behavioral rule; temporary pod names, incident counts, and local
  workarounds belong in the task report.
- Reconcile guidance in the same change as the behavior when practical. Remove
  superseded advice instead of appending competing rules. Update root
  `AGENTS.md` only if skill discovery or task routing changes.
- Validate frontmatter with `skill-creator`'s `scripts/quick_validate.py` when
  available. Check changed Markdown links and command references against the
  repository, `make help`, and implementation without executing mutations. Use a
  configured pre-commit link hook when present; otherwise a temporary local
  hook can check relative paths and anchors without adding a repository-wide
  dependency. Preserve the existing automatic invocation policy.
- If no durable lesson or drift is found, leave the skill unchanged. Report an
  update briefly when made; flag an unresolved discrepancy rather than inventing
  guidance. Skill maintenance does not authorize extra runtime actions or
  require re-approval for work already authorized.
