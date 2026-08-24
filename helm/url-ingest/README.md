# URL ingest Helm chart

This chart owns the five scheduled crawlers, their service account, URL-list
ConfigMaps, and retained NFS claims. It deliberately does not create Secrets,
stage corpus files, or run ingestion jobs as Helm hooks.

Install it through the repository root so the two large URL lists are supplied
with `--set-file`. Schedules default to suspended; enable them only after the
fresh corpus has passed collection validation.

PDF batches default to two documents so shared external extraction NIMs are
not flooded by large files; URL batches retain their collection-specific
throughput settings.

`ingestion.globalCapacity` controls a slot semaphore on the shared manifest
PVC. Scheduled jobs default to one slot. Root manual runs set the capacity on
their immutable Jobs to `MAX_PARALLEL_INGEST_SHARDS`, so the documented shard
parallelism and the runtime lock cannot drift apart.
