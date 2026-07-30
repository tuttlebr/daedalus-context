# Executive Summary: k8s-multicluster-mcp

## Assessment

This repository is an MCP server and Kubernetes deployment component supporting `daedalus-agent`. It exposes Kubernetes, Helm, GPU diagnostics/remediation, optional dangerous capabilities, and an opencode-backed DevOps agent through MCP tools.

Overall readiness: critical/high code review findings have been remediated. A cluster integration review is still required before broader production reliance.

Current local verification passed:

- `make test`
- `make check`

## Remediated Risks

1. Default Kubernetes deployment is read-only, authenticated, and uses in-cluster ServiceAccount credentials.
2. Agent-facing structured and text outputs pass through shared secret redaction.
3. `execInPod`, `runKubectlCommand`, and agent write mode require explicit capability gates.
4. Process-global `switchContext` is no longer registered.
5. Create/update paths now use server-side apply and YAML kind inference.
6. The release image now bakes a curated, audited DevOps-engineer skill set (Kubernetes, SRE, debugging, Python/Rust, NVIDIA NIM stack) plus the runtime toolchain those skills assume (`git`, `github-cli`, `python3`, `py3-yaml`, `jq`); deployments can still override via a runtime volume mount on `/home/appuser/.config/opencode/skills`.

## Decision

Treat this as internal platform infrastructure after a hardening pass.

Production-like reliance should now be gated on integration verification: deploy to a test cluster, verify HTTP auth, confirm default read-only tool listing, exercise redaction with synthetic secrets, and test rollback.

## Immediate Release-Blocking Criteria

- Deployed default is read-only unless an operator intentionally enables write mode.
- ServiceAccount RBAC and kubeconfig behavior cannot accidentally grant broader write access than the deployment contract states.
- Raw secret-bearing outputs are redacted or gated.
- Dangerous MCP tools are permission-scoped, disabled by default, or explicitly policy-gated.
- The HTTP endpoint has an explicit access-control story for non-localhost usage.
