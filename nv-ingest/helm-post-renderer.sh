#!/usr/bin/env bash
set -euo pipefail

# Apply the chart customizations that the upstream NV-Ingest chart does not
# expose as values:
# - use a no-surge rollout for the large, anti-affined API workers so Helm does
#   not wait forever for an unschedulable extra replica;
# - during the one-time NGC Secret migration, keep the rendered fixed-name
#   Secrets so the following release can stop owning them without deleting the
#   live externally managed copies.
awk '
  function emit_document() {
    if (document == "") {
      return
    }

    if (document ~ /\nkind:[[:space:]]*Deployment[[:space:]]*\n/ &&
        document ~ /\n    helm.sh\/chart:[[:space:]]*nv-ingest-/ &&
        document ~ /\n    app.kubernetes.io\/name:[[:space:]]*nv-ingest[[:space:]]*\n/) {
      if (document ~ /\n  strategy:[[:space:]]*\n/) {
        print "The upstream NV-Ingest chart now renders a Deployment strategy; review the post-renderer." > "/dev/stderr"
        invalid = 1
      } else if (!sub(/\nspec:[[:space:]]*\n/,
                      "\nspec:\n  strategy:\n    type: RollingUpdate\n    rollingUpdate:\n      maxUnavailable: 1\n      maxSurge: 0\n",
                      document)) {
        print "Unable to add the NV-Ingest Deployment rollout strategy." > "/dev/stderr"
        invalid = 1
      } else {
        deployment_count++
      }

      # NV-Ingest 26.3 emits malformed YAML for extraVolumeMounts. Add the
      # mounts here only when the owned wrapper supplies the matching volumes.
      if (document ~ /name:[[:space:]]*local-pdfium[[:space:]]*\n/) {
        mounts = "\n          volumeMounts:\n"
        mounts = mounts "            - name: local-pdfium\n"
        mounts = mounts "              mountPath: /opt/nv_ingest_runtime/lib/python3.12/site-packages/nv_ingest_api/internal/extract/pdf/engines/pdfium.py\n"
        mounts = mounts "              subPath: local_pdf.py\n              readOnly: true\n"
        mounts = mounts "            - name: local-nemotron\n"
        mounts = mounts "              mountPath: /opt/nv_ingest_runtime/lib/python3.12/site-packages/nv_ingest_api/internal/extract/pdf/engines/nemotron_parse.py\n"
        mounts = mounts "              subPath: local_pdf.py\n              readOnly: true\n"
        if (!sub(/\n          volumeMounts:[[:space:]]*\n/, mounts, document)) {
          print "Unable to mount the local document extraction engine." > "/dev/stderr"
          invalid = 1
        }
      }
    }

    if (document ~ /\nkind:[[:space:]]*Secret[[:space:]]*\n/ &&
        document ~ /\n  name:[[:space:]]*ngc-(secret|api)([[:space:]]|#|$)/) {
      sub(/\nmetadata:[[:space:]]*\n/,
          "\nmetadata:\n  annotations:\n    helm.sh/resource-policy: keep\n",
          document)
    }

    printf "%s", document
  }

  /^---[[:space:]]*$/ {
    emit_document()
    document = $0 ORS
    next
  }

  {
    document = document $0 ORS
  }

  END {
    emit_document()
    if (invalid || deployment_count != 1) {
      if (!invalid) {
        print "Expected exactly one NV-Ingest Deployment in the rendered chart." > "/dev/stderr"
      }
      exit 1
    }
  }
'
