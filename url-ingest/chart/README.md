# URL Refresh Chart

This chart owns collection refresh CronJobs, the monitor, service account,
contract/URL-list ConfigMaps, and retained URL-data and manifest claims. It
references the PDF server's claim and externally managed Secrets. It does not
publish corpus files or run ingestion as a Helm hook.

Deploy through root `make deploy COMPONENTS=url-ingest` after the
[root configuration setup](../../docs/deployment.md#configuration). The root recipe
regenerates the refresh contract from the retrieval schema, collection contract,
and source lists, and supplies the large URL lists with `--set-file`.

[values.yaml](values.yaml) owns resources, schedules, retries, work budgets,
pacing, and the shared extraction capacity. Each CronJob runs `refresh.py` in
the runner image. Manual refresh Jobs inherit that template and its capacity;
there is no Rust crawler init container in this chart.

Schedules default to suspended. After corpus acceptance, enable them through
`make enable-schedules` and persist `URL_INGEST_SUSPEND=false` in the root `.env`.

Use [URL refresh operations](../README.md) for data submission,
status, verification, and source-replacement behavior. Root
`make validate-helm` validates the active chart and refresh contracts.
