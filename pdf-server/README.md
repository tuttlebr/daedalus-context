# Daedalus PDF Corpus Server

The [PDF chart](chart/README.md) owns the HTTP service and retained
corpus PVC in the shared `daedalus` namespace. This component supplies the image
and publication tooling. URL refresh consumes the `pdf-server-data` claim
read-only and does not reconcile PDF resources.

## Deploy and publish

Use the [root configuration setup](../README.md#configuration), then run from
the repository root:

```bash
make deploy COMPONENTS=pdf-server
make health-provider COMPONENTS=pdf-server
```

The root [image specification](../docker-compose.yaml) and
[chart values](chart/values.yaml) own the image and service settings.
Build and publish images through the root image workflow before deployment.
Use `make render` in this folder for local inspection of the same chart.
The publication helper `publish-corpus.sh` is invoked by root `make publish-corpus`.

Publishing is a separate data operation. Choose a unique source revision or
digest as the corpus generation:

```bash
make publish-corpus CORPUS_GENERATION=corpus-v1
make refresh-run COLLECTIONS="nvidia mentalhealth"
```

The publication Job copies to `.generations/<generation>`, writes and verifies
`manifest.sha256`, and atomically switches the `current` pointer. Stable
per-collection links resolve through that pointer. A publish lock prevents
concurrent writers; a monotonic fencing token rejects an older delayed Job.
Existing physical collection directories cannot be overwritten by publication.

Source paths and the node holding them are configured through
`PDF_SOURCE_HOST_PATH` and `PDF_SOURCE_NODE`. Do not copy files directly into
the active PVC. Publication selects the corpus; refresh Jobs separately update
retrieval data and return from submission before ingestion completes.

The default HTTP path is `http://pdf-server.daedalus.svc.cluster.local/pdfs/`.
The backing RWX store is a shared failure domain. Multiple HTTP replicas protect
pod availability but do not make a single NFS appliance highly available.

## Validation

Run `make test` from this directory for publication tests. Use the root
`make validate-helm` for chart validation and `make validate-local` for executable
source changes. A successful HTTP probe alone does not verify corpus contents
or retrieval.
