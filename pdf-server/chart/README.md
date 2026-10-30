# PDF Server Chart

This chart owns the read-only HTTP service and retained corpus PVC.
[values.yaml](values.yaml) defines image, storage, service, and placement.

After the [root configuration setup](../../README.md#configuration), use root
`make deploy COMPONENTS=pdf-server`. Publishing a corpus is a separate
`make publish-corpus CORPUS_GENERATION=<source-revision-or-digest>` data operation.

See [PDF publication](../README.md) for generation selection,
concurrency, and validation. Root `make validate-helm` validates this chart.
