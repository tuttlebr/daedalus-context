# Remediation Roadmap

## Immediate: 0 to 2 Weeks

Goal: reduce operational and security risk before production-like use.

### Release Blockers

1. Close `RISK-001`: make the default Kubernetes deployment read-only and least-privilege.
2. Close `RISK-002`: sanitize logs, Helm release output, and all raw text outputs.
3. Close `RISK-003`: disable or tightly policy-gate `runKubectlCommand`.
4. Close `RISK-004`: remove or redesign process-global context switching.
5. Close `RISK-005`: add an explicit access-control model for HTTP MCP usage.

### Executable Work

- Add `--read-only` to the default Kubernetes manifest unless a separate write-mode overlay is used.
- Remove the mounted kubeconfig from the default in-cluster deployment, or document and enforce why it is required.
- Add a response sanitizer for raw text and Helm release objects.
- Add tests with synthetic short and long secret values in logs, Helm values, and Secret manifests.
- Split dangerous tools into separate capability gates: `--enable-kubectl`, `--enable-exec`, `--enable-agent-write`.
- Add structured audit logs for tool name, capability class, namespace, target, request ID, status, duration, and error category.

## Near-Term: 2 to 6 Weeks

Goal: make the component repeatable and supportable.

- Replace merge-patch create/update behavior with server-side apply or adjust tool descriptions and tests to match actual semantics.
- Add per-tool timeouts and bounded list defaults.
- Add pagination for broad Kubernetes list operations.
- Add MCP tool contract tests that verify read-only mode omits write tools.
- Add fake-client tests for mutation paths: create/update, delete, scale, restart, GPU remediation.
- Add deployment checks for resources, probes, ServiceAccount, RBAC, NetworkPolicy, and security context.
- Align README deployment docs with actual manifests.
- Decide whether this repo should ship a Helm chart or Kustomize overlays for read-only and write-enabled modes.

## Strategic: 6 to 12 Weeks

Goal: make this a maintainable platform component.

- Define a common MCP platform policy model for `daedalus-agent` support tools.
- Publish capability standards for agent-facing tools.
- Add policy-as-code checks for Kubernetes manifests.
- Add performance baselines for large clusters.
- Add compatibility tests for supported Kubernetes and Helm versions.
- Create architecture decision records for multi-cluster session behavior, dangerous tool policy, and secret redaction guarantees.
- Build a release checklist shared across related platform repos.

## Suggested GitHub Issue Backlog

### Theme: Secure Default Deployment

Parent issue: Make k8s MCP deployment safe by default

Child issues:

- Add read-only default deployment mode.
- Remove or constrain kubeconfig secret mount.
- Add write-enabled overlay with explicit warning and RBAC requirements.
- Add manifest tests or policy checks for read-only defaults.

Acceptance criteria:

- Fresh apply of `deploy/k8s-mcp-server.yaml` does not register write tools.
- The default ServiceAccount cannot mutate cluster state.
- README documents how to intentionally enable write mode and its risk.

### Theme: End-to-End Secret Safety

Parent issue: Ensure agent-facing outputs cannot leak secrets by default

Child issues:

- Sanitize `getPodsLogs` output.
- Sanitize `getPodDebugInfo` logs.
- Redact Helm release values and rendered Secret manifests.
- Add regression tests for short tokens, base64 values, and rendered Secret YAML.

Acceptance criteria:

- Synthetic secrets do not appear in any MCP response under default settings.
- Raw output access, if retained, requires a distinct unsafe capability gate.

### Theme: Dangerous Tool Policy

Parent issue: Permission-scope side-effecting and escape-hatch MCP tools

Child issues:

- Add separate flags or policy config for `execInPod`, `runKubectlCommand`, GPU remediation, Helm write tools, and agent write mode.
- Return machine-readable denial errors when a capability is disabled.
- Add audit logging for all side-effecting calls.

Acceptance criteria:

- Each dangerous capability can be independently disabled.
- Denied calls are structured and explain required capability.
- CI verifies default mode excludes dangerous tools.

### Theme: Multi-Cluster Safety

Parent issue: Replace process-global context switching with request-safe cluster targeting

Child issues:

- Define cluster selection model.
- Build immutable client registry keyed by context.
- Add concurrent request tests.
- Deprecate or remove global `switchContext`.

Acceptance criteria:

- Two concurrent MCP requests can target different contexts without shared mutation.
- A context change cannot affect unrelated clients or sessions.
