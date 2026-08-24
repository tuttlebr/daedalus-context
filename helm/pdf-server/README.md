# PDF server chart

```sh
helm upgrade --install pdf-server helm/pdf-server \
  --namespace daedalus --create-namespace --atomic --timeout 10m
```

The release owns the read-only service and retained corpus PVC. Publishing a
new corpus generation is deliberately handled by `make publish-corpus`.
