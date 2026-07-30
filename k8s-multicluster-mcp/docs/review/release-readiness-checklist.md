# Release Readiness Checklist

Use this before relying on this component beyond local or isolated-cluster usage.

## Gate 1: Remediation Review

- [ ] All critical risks in `risk-register.csv` are closed or explicitly accepted.
- [ ] All high risks in `risk-register.csv` are closed or explicitly accepted.
- [ ] Default deployment is read-only.
- [ ] Write-enabled deployment requires explicit configuration.
- [ ] Dangerous tools have independent capability gates.
- [ ] Secret redaction tests cover Kubernetes resources, logs, Helm release values, and rendered manifests.
- [ ] Tool denial errors are machine-readable.
- [ ] README and deployment manifests describe the same operational model.

## Gate 2: Integration Review

- [ ] Deploy to a test Kubernetes cluster from a clean checkout.
- [ ] Verify `/healthz` reports Kubernetes API connectivity.
- [ ] List tools and confirm default read-only mode omits write tools.
- [ ] Invoke representative read tools with valid input.
- [ ] Invoke representative tools with malformed input and verify structured errors.
- [ ] Verify logs and Helm outputs do not expose synthetic secrets.
- [ ] Exercise write mode in an isolated namespace with least-privilege RBAC.
- [ ] Verify rollback or cleanup path for deployment changes.
- [ ] Confirm audit logs contain tool name, target, request ID, status, duration, and error category.

## Gate 3: Release Readiness Review

- [ ] Ownership and on-call path are documented.
- [ ] Runbook covers deploy, rollback, credential rotation, and incident response.
- [ ] CI is green for format, vet, tests, build, lint, Docker build, Trivy, and govulncheck.
- [ ] Image tags are controlled and suitable for the target environment.
- [ ] Network exposure is explicit and reviewed.
- [ ] RBAC is least privilege for the selected deployment mode.
- [ ] Known risks are documented with owner, target release, and acceptance decision.
- [ ] Architecture decisions exist for multi-cluster behavior, capability policy, and redaction guarantees.
