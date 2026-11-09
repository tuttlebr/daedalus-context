# Code Review and Remediation

Read this for repository-wide review, focused change review, bug investigation,
or source fixes. Operational authorization and compatibility constraints remain
in [SKILL.md](../SKILL.md). Run commands from the repository root unless noted.

## Establish Scope and Coverage

For a diff review, establish the requested base and head and inspect staged,
unstaged, and relevant untracked work. Follow callers, consumers, and tests
beyond the diff where necessary. Distinguish introduced regressions from
pre-existing defects. For a full review without a revision range, use the
current working tree and cover all owned source surfaces below; do not silently
reduce the task to recent changes or root scripts.

Use [topology](../../../../stack/topology.yaml), the
[component lock](../../../../stack/components.lock.yaml), and
[image build contexts](../../../../docker-compose.yaml) to establish ownership.
Inspect applicable component instructions and current test recipes. Adjacent
repositories and third-party dependencies are integration boundaries; inspect
the necessary contracts, but do not expand fixes into them without task scope.
Keep a coverage record for broad reviews, including unavailable code and
unverified paths, so the handoff states what was actually examined.

| Surface | Code and contracts to trace | Local validation entry points |
| --- | --- | --- |
| Root orchestration and recovery | [Makefile](../../../../Makefile), [scripts](../../../../scripts/), [stack](../../../../stack/) | [scripts/tests](../../../../scripts/tests/), `make validate-context` |
| Runtime and image configuration | [Service chart inventory](../../../../README.md#stack-model), service-owned charts, [Compose image specification](../../../../docker-compose.yaml), Dockerfiles | `make validate-helm`, `make images-validate`, chart tests in [nv-ingest/tests](../../../../nv-ingest/tests/) |
| URL/PDF ingestion and refresh | [url-ingest](../../../../url-ingest/): Rust crawler, Python workers, durable state, job renderers | [url-ingest/tests](../../../../url-ingest/tests/), Rust checks from its current Makefile/Dockerfile |
| Retrieval compatibility | [nemotron-retriever](../../../../nemotron-retriever/): adapters, templates, chart values | [nemotron-retriever/tests](../../../../nemotron-retriever/tests/), `make -C nemotron-retriever test` |
| PDF corpus publication and serving | [pdf-server](../../../../pdf-server/): publication scripts, manifests, HTTP configuration | [pdf-server/tests](../../../../pdf-server/tests/), `make -C pdf-server test` |
| Kubernetes and UniFi tools | [k8s-multicluster-mcp/src](../../../../k8s-multicluster-mcp/src/), [unifi-network-mcp/src](../../../../unifi-network-mcp/src/): handlers, clients, tools, authentication | `make -C k8s-multicluster-mcp check`, `make -C unifi-network-mcp test` |

These are navigation aids, not a frozen inventory. Follow new owned components
and configuration introduced since this guide was written. Read recipes before
running them: `make validate-local` includes Docker builds and, when selected,
adjacent Hindsight validation; it is not a lightweight unit-test alias. Source tests can
use temporary files and mocked clients without loading `.env` or contacting
live services. Exercise root orchestration with fake clients and render service-owned charts
locally; do not deploy, crawl external sites, or ingest live data as part of a
unit test.

## Identify and Substantiate Defects

Trace configuration from authoritative inputs through rendering and runtime
use, then trace requests and data across component boundaries. Examine failure
paths as well as success paths. Prioritize concrete correctness, data integrity,
security, reliability, and performance defects over stylistic changes.

Use these repository contracts to guide investigation:

- Root orchestration must preserve dependency ordering, failure propagation,
  exit status, and the wrong-context guard. Health reporting must distinguish
  provider readiness, dependency health, freshness, and consumer functionality.
- Source deployment must build/push only selected locally owned image contexts,
  propagate the exact unique tags into recursive Make/Helm, and stop rollout on
  failed publication. `scripts/tests/test_deploy_source.py` exercises executable
  fake clients, including failure ordering and unchanged schedule/config inputs.
  Upstream images and external Hindsight keep their existing ownership. Do not
  implement source refresh by deleting storage or launching ingestion Jobs.
- Compare chart defaults, root overrides, generated settings, and actual client
  contracts. Consult [schemas](../../../../stack/schemas/),
  [RAG schema](../../../../stack/rag-schema.yaml), and
  [collection contracts](../../../../stack/collections.yaml) for endpoint,
  embedding, dimension, metric, and generation compatibility.
- Read the actual Agent consumers when changing generated settings. A sourceable
  environment file is insufficient if it only emits a URL while the consumer
  reads separate host/port variables. Keep existing URL keys compatible, test
  topology changes, and regenerate both checked-in environment outputs. The
  owning regression is `AgentEnvironmentTests` in `scripts/tests/test_contextctl.py`.
- Exercise both role-aware Agent embeddings and NIM input requests without
  manually adding template-owned prefixes. Provider probes from the Agent pod
  establish API/network evidence, not execution of the Agent's retrieval tool
  or cited model continuation. `scripts/tests/test_retriever_probe.py` checks
  probe payloads and rejects invalid vectors with matching dimensions.
- Check the topology's failure policy: optional reranking and tool failures must
  not disable core chat; ingestion failure must not erase existing search data.
- For ingestion, locking, retries, replacements, deletion, freshness, or model
  migrations, also read [Refresh operations](refresh-operations.md). Reproduce
  state changes with disposable fixtures; constructing the state helper against
  production state can write even during apparent inspection.
- Review MCP authentication, authorization, tool side effects, subprocess and
  path handling, HTTP/TLS behavior, secret redaction, timeouts, and cancellation
  where the implementation exposes them. Assess security fixes against working
  LAN clients and service connections before changing access contracts.

For each finding, establish a reachable trigger, the violated contract, the
observed or demonstrable consequence, and the responsible code location. Prefer
a minimal failing test or isolated reproducer; static control/data-flow evidence
is sufficient when execution needs unavailable dependencies or live mutations.
Do not present a suspicion, outdated comment, or scanner warning as a confirmed
bug without checking the implementation and caller assumptions.

Report findings by severity: P0 for immediate critical impact, P1 for high-impact
defects requiring prompt attention, P2 for normal-priority bugs, and P3 for minor
defects. Include file and line, trigger, impact, evidence, and a repair direction.
State unverified assumptions separately. If no defects are confirmed, say so
and report coverage and test limits; passing tests do not prove the absence of
bugs. Honor any review format explicitly requested by the user.

## Remediate and Validate

When remediation is requested, fix confirmed in-scope defects through the owning
source and authoritative configuration. Preserve existing API, CLI, storage,
and LAN contracts unless a change is explicitly approved. Keep patches focused
on the root cause, update dependent callers/contracts when required, and avoid
unrelated refactors. Follow a confirmed bug across components when needed for a
complete fix; component boundaries alone are not a reason to stop.

For behavioral fixes, add or adapt a focused regression test that fails before
the fix and passes afterward when a practical local test exists. Exercise the
trigger and externally meaningful outcome, including the relevant failure or
recovery path. Do not weaken assertions or suppress errors merely to pass the
gate. Use temporary state and fake services for data-loss, concurrency, retry,
and external-tool scenarios; never clear live state to reproduce a defect.

Run the relevant component checks, then the validation gates in
[SKILL.md](../SKILL.md#validate-and-hand-off). Inspect the resulting diff for
compatibility regressions and unintended generated artifacts. Record command
outcomes and distinguish baseline failures or missing prerequisites from failures
caused by the patch; finish feasible local checks even when a broader gate is
blocked. A source fix can be complete while deployment or live acceptance remains
pending, and the handoff must make that distinction explicit.

For an authorized live fix, update durable configuration alongside live changes
and use the operational workflow's rollback and client-path verification. Source
remediation alone does not authorize publishing images, restarting workloads,
running data jobs, or migrating collections. If a remaining fix needs a
compatibility break or additional operational authorization, complete the
reviewable source work that is safe within scope and describe the concrete
remaining action and impact; do not repeat requests for permission already given.

Lead the handoff with findings for review-only work, or repaired behavior for
remediation. Include unresolved findings, coverage limits, validation results,
and any deployment/data work still needed. Do not imply a live service has been
repaired merely because a local patch passes its tests.
