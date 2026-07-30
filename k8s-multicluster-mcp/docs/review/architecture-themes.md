# Architecture Themes Memo

## Component Role

`k8s-multicluster-mcp` should be treated as an internal platform component for `daedalus-agent`.

It is not just a utility process. It mediates agent access to Kubernetes and Helm, which means its real contract is:

- expose narrow, predictable MCP tools;
- bound and audit side effects;
- sanitize data before it reaches an agent;
- preserve operator control over cluster scope, credentials, and write permissions;
- fail in ways that are observable and recoverable.

## Systemic Themes

### Permission Scope Is Not Yet a First-Class Contract

The code has a useful `--read-only` registration gate, but deployment and tool-level policy are not yet strong enough. Write mode currently changes the whole server surface, and dangerous tools are not separately scoped.

Decision needed: define capability classes such as `read`, `write`, `exec`, `admin`, `agent`, and `raw-output`, then enforce them consistently.

### Secret Redaction Is Partial

Structured Kubernetes resources go through `SanitizeResource`, but raw text and Helm release objects are not consistently sanitized. Agent-facing systems need redaction at every output boundary, not just selected resource paths.

Decision needed: all handlers should return through a shared response safety layer that knows how to sanitize structured Kubernetes objects, text, logs, Helm manifests, and error strings.

### Multi-Cluster Behavior Needs a Session Model

The current `switchContext` behavior is process-global. That is tolerable for a local CLI-style server but unsafe for concurrent HTTP usage. Multi-cluster operation should be explicit per request, per session, or per configured server instance.

Decision needed: choose one multi-cluster model and remove ambiguous global mutation.

### Declarative Intent Is Ahead of Implementation

Tool descriptions and README language imply server-side apply and safe create-or-update workflows. The implementation currently uses merge patch in key paths. That is operationally different and can leave drift.

Decision needed: implement true server-side apply or adjust the contract and acceptance tests to match merge-patch semantics.

### Operational Controls Are Uneven

The repo has strong CI basics and Kubernetes probes, but it lacks a consistent model for request tracing, tool-call audit logs, per-tool timeouts, pagination, and production policy gates.

Decision needed: define a minimum MCP platform standard covering logging fields, correlation IDs, timeouts, pagination, and authorization.

## Recommended Platform Standards From This Review

- Default deployment is read-only.
- Write, exec, direct kubectl, raw logs, and autonomous agent actions are independent capabilities.
- Every MCP response path passes through a safety layer before returning to the client.
- Multi-cluster targeting is explicit and immutable within a request.
- Broad list tools require bounds or selectors.
- Every side-effecting tool returns structured status including target, action, changed state, and verification guidance.
- CI includes contract tests for tool schemas, read-only mode, sanitizer behavior, and deployment manifests.
