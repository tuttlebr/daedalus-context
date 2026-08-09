# Daedalus PDF Corpus Server

This release is the sole owner of the Kubernetes PDF corpus resources:
ServiceAccount, PVC, nginx ConfigMap, Deployment, Service, PDB, NetworkPolicy,
and publication Job. URL ingest consumes the `pdf-server-data` claim and
`pdf-server` Service when deployed in the same namespace; it does not reconcile
them.

The workspace root owns the image release and Kubernetes deployment paths:

```bash
cd ..
make images-build
COMPONENTS=pdf-server make deploy
```

The root image specification wraps the versioned `nginx:1.27.4-alpine` base as
`daedalus.ddns.me:5050/btuttle/pdf-server:0.1.0`. The same SemVer-tagged
`PDF_SERVER_IMAGE` is used by the Kubernetes deployment.

Use the component wrapper only for focused debugging or recovery:

```bash
TARGET=kubernetes NAMESPACE=milvus ./deploy.sh
```

Publishing is an explicit data mutation:

```bash
TARGET=kubernetes \
PUBLISH_CORPUS=true \
CORPUS_GENERATION=<source-revision-or-digest> \
PDF_SOURCE_NODE=daedalus-01 \
./deploy.sh
```

The Job copies into `.generations/<generation>`, writes and verifies
`manifest.sha256`, and atomically switches one `current` generation pointer.
Stable per-collection links resolve through that pointer, so every collection
changes generation together. An atomic publish lock prevents concurrent
writers, and a monotonic fencing token rejects a delayed older Job after a
newer generation has already won. The publisher refuses to overwrite a legacy
physical collection directory. Move legacy data into the generation layout
only during an approved migration with backup and rollback.

The backing RWX store remains an external failure domain until its HA,
snapshot, and restore contract is proven. Two nginx replicas protect pod and
voluntary-disruption availability; they do not make a singleton NFS endpoint
highly available.
