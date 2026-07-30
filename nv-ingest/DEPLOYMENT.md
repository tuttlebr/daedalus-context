# NV-Ingest RAG Deployment

This directory is the deployment wrapper for the NV-Ingest service used by the
Daedalus retrieval stack. It deploys the service only; URL and PDF population
is owned by `../url-ingest`.

The production profile pins both the Helm chart and application image to
`26.3.0`, runs three API replicas, prefers spreading those replicas across
hosts, and exposes the service as an in-cluster `ClusterIP` through the root
Makefile. Milvus and MinIO remain external to the NV-Ingest chart.

## Configure and deploy

Copy `.env.example` to `.env`, fill the NGC and remaining extraction endpoint
settings, and do not commit the resulting file. The embedding defaults point
to the `nemotron-retriever` vLLM Service in `daedalus-retriever`; deploy that
chart before NV-Ingest. Its compatibility adapter translates NV-Ingest's NIM
embedding payload to the role prefixes required by
`nvidia/llama-nemotron-embed-vl-1b-v2`.

The recommended object-store configuration copies credentials from the
authoritative Milvus MinIO Secret:

```dotenv
MINIO_SOURCE_SECRET_NAMESPACE=milvus
MINIO_SOURCE_SECRET_NAME=milvus-minio-credentials
MINIO_CREDENTIAL_SECRET_NAME=nv-ingest-object-store
```

The deploy reads `accesskey` and `secretkey` from one source-Secret snapshot,
creates the destination Secret in the NV-Ingest namespace, and references that
Secret from the Deployment. In a new or fully migrated release, NGC pull/API
credentials and MinIO credentials are not rendered into Helm values or stored
in the current Helm release manifest. Secret resource-version checksums on the
pod template make credential rotation trigger a rollout without exposing
secret contents.

From the workspace root, use:

```bash
make deploy COMPONENTS="retriever nv-ingest"
make healthcheck COMPONENTS="retriever nv-ingest"
```

For a direct deployment from this directory, use `./deploy.sh`. The script
authenticates and renders the pinned chart before it mutates the namespace. It
also rejects malformed settings, incomplete MinIO credentials, destination
Secret name collisions, and unmarked pre-existing Secrets. Do not enable
`ADOPT_EXISTING_SECRETS` unless the named objects were inspected and the
rollout is intentionally taking ownership of them.

`nv-ingest/deploy.sh --run-now` is intentionally unsupported. After the
service is healthy, use `../url-ingest/deploy.sh --run-now <collection>` or the
root `make seed-data` workflow.

## Readiness semantics

Leave `COMPONENTS_TO_READY_CHECK` empty when OCR, YOLOX, VLM, or parser
endpoints are authenticated external NVCF services. Their unauthenticated
health responses do not necessarily implement the JSON contract expected by
NV-Ingest's component readiness parser. In that profile, use the NV-Ingest
pipeline readiness probe plus the authenticated seed preflight and real
ingestion requests as the operational signal.

Set `COMPONENTS_TO_READY_CHECK=ALL` only when every configured dependency
exposes compatible readiness JSON without additional authentication. Enabling
`ALL` against incompatible external health endpoints can keep an otherwise
usable service unready.

## Safe rollout guard

A routine deployment refuses to proceed while either of these exists in
`URL_INGEST_NAMESPACE`:

- an active Job labeled `app.kubernetes.io/name=url-ingest`;
- a `url-ingest-*-run-lock` ConfigMap.

Kubernetes replica readiness does not preserve the in-flight Ray work owned by
a terminating API pod. Wait for both the population Job and its lock to clear,
then deploy. `ALLOW_DEPLOY_DURING_INGEST=true` is an emergency override only;
using it can force the manifest-backed seed workflow to retry unfinished work.
The root Makefile exposes the same override as
`ALLOW_NV_INGEST_DEPLOY_DURING_INGEST=true`.

## One-time Secret migration and rollback

Older chart revisions rendered `ngc-secret` and `ngc-api` as Helm-owned
resources. The wrapper migrates them without a delete/recreate window:

1. an atomic transition revision retains the Secrets and adds Helm's keep
   policy;
2. a forward-only revision removes Secrets from the release manifest;
3. after that revision succeeds, ownership is marked external;
4. an atomic Secret-free revision reconciles the resource-version checksum.

If the forward-only step fails, rerun the deploy; it resumes the migration
without externalizing the Secrets prematurely. Do not manually roll back
across the ownership boundary. First inspect the target revision and confirm it
does not render either fixed-name Secret:

```bash
helm get manifest nv-ingest -n nv-ingest --revision <revision>
```

For the current Daedalus cluster history, revision `5` is the first known
Secret-free revision; revision `4` and older are unsafe rollback targets. Old
Helm history may contain historical NGC credentials even after the live
manifest is Secret-free, so access to Helm release Secrets must be treated as
sensitive. Routine revisions after the migration are atomic.

## Validate

Run the behavior tests and render validation before deploying:

```bash
bash tests/test-deploy.sh
./validate.sh
```

`tests/test-deploy.sh` uses fake Helm and Kubernetes clients to cover the
active-ingestion refusal, pre-mutation failures, Secret ownership migration,
interrupted-migration recovery, checksum-driven credential rollout, and the
post-renderer scope.

`validate.sh` verifies that the `26.3.0` chart renders a ClusterIP Service,
release-scoped anti-affinity, external Secret references, rollout checksums,
and no literal credential values or Helm-managed Secrets. It uses a cached
chart when available. Otherwise set `NGC_API_KEY`; for an offline run, set
`NV_INGEST_VALIDATION_CHART` to an authenticated local chart archive.

The workspace-wide equivalent is `make validate` from the repository root.

## Operational checks

```bash
kubectl get pods,svc -n nv-ingest
kubectl port-forward -n nv-ingest svc/nv-ingest 7670:7670
curl --fail http://127.0.0.1:7670/v1/health/ready
```

If a population run stalls, inspect the seed Job first, then the NV-Ingest API
and Redis logs. Do not roll out NV-Ingest as a timeout workaround while a seed
Job is active; the URL-ingest client and Job deadlines are responsible for
bounded retries and resumable population.
