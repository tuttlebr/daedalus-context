# Publication and releases

The source tree is developed as one Daedalus Context repository. A public source
release, a public image release, and a production rollout are separate actions.
Use [Contributing](../CONTRIBUTING.md) for local acceptance and
[deployment](deployment.md) for the runtime contract.

## Source publication

Before making a repository public, review both the current tree and the Git
history that will become visible. Deleting a file in the latest commit does not
remove earlier copies. Scan locally reachable history and inspect all branches,
tags, Git LFS objects, release assets, and attached artifacts intended for
publication. A clean signature scan is supporting evidence, not proof that all
private material has been removed.

Keep scanner reports private: they can contain matched credentials or sensitive
text. If a credential is confirmed, rotate it and separately plan any required
history rewrite; do not silently rewrite shared history as part of documentation
cleanup. Do not commit `.env`, kubeconfigs, private keys, downloaded corpora, or
operational backups.

The reference configuration intentionally names the Daedalus LAN, node layout,
storage, registry, and public trust certificate. Decide whether those identifiers
should be public. Preserve working deployment configuration when preparing
examples; use reviewed overrides for another cluster. The
[example environment](../.env.example) is a template, not working credentials or
access to the author's infrastructure.

## Licensing and provenance

A root license for original code has not been selected. Do not advertise the
repository under an assumed MIT or Apache license, and do not replace upstream
notices with a repository-wide assertion of ownership.

Before distributing third-party material, establish its provenance and applicable
terms, including:

- the [reranker template notices](../nemotron-retriever/chart/files/nemotron-rerank-notices/THIRD_PARTY_NOTICES.md)
  and accompanying license/notice files;
- the pinned upstream Helm archives in the Milvus and NV-Ingest charts;
- the UniFi OpenAPI material in [unifi-network-mcp/src](../unifi-network-mcp/src/),
  whose embedded license information does not currently establish redistribution
  terms;
- downloaded parser/model code and weights included in image builds; and
- source documents and generated datasets, whose rights are separate from code.

Do not remove API contracts or test fixtures merely to eliminate a provenance
question. Resolve that question, replace the material through a reviewed change,
or exclude the affected artifact from the intended release.

## Reproducible release path

1. Run the local and CI checks against the exact source revision to publish.
2. Review the [component lock](../stack/components.lock.yaml), vendored charts,
   model revisions, and generated consumer/refresh contracts together.
3. Decide which artifacts are source-only and which images will be distributed.
   The reference private registry is not a public download channel.
4. Build changed owned images under new immutable SemVer tags in an accessible
   registry. `make images-release-refs` prints deployment assignments; it does
   not update `.env` or the lock automatically.
5. Verify the selected platform manifests and pull permissions. The pinned
   MinIO mirror is external to the six owned image builds and needs its own
   distribution decision.
6. Record validation limits and installation prerequisites in the release notes.
   Only claim GPU extraction, cluster recovery, or Agent integration when those
   paths were actually tested.

The [CI security jobs](../.github/workflows/ci.yaml) run Go vulnerability and
filesystem scans. Reproduce findings with the toolchain declared in each
`go.mod` and its image build, distinguish reachable vulnerabilities from scanner
inference, and update the toolchain/dependencies with compatibility tests.
Passing source tests does not satisfy a failing security gate; do not disable
that gate to label a release ready.

Public CI checks source and rendered contracts without production secrets or
cluster access. Runtime acceptance belongs to a separately configured cluster.
The default Hindsight integration also needs its owning checkout; source
contributors can use `make validate-context` without it.
