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
