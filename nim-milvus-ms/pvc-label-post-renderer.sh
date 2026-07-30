#!/usr/bin/env bash

set -euo pipefail

# The upstream etcd and MinIO subcharts do not put labels on their
# volumeClaimTemplates. Kubernetes copies these labels to every PVC it creates,
# giving the lifecycle scripts a release-scoped, component-scoped selector that
# remains usable after Helm deletes the StatefulSets.
: "${MILVUS_PVC_RELEASE_NAME:?MILVUS_PVC_RELEASE_NAME must be set}"

awk -v release="${MILVUS_PVC_RELEASE_NAME}" '
  function reset_document() {
    is_statefulset = 0
    component = ""
    in_claim_templates = 0
  }

  BEGIN {
    reset_document()
  }

  /^---[[:space:]]*$/ {
    print
    reset_document()
    next
  }

  /^kind:[[:space:]]*StatefulSet[[:space:]]*$/ {
    is_statefulset = 1
  }

  is_statefulset && /^[[:space:]]+app\.kubernetes\.io\/name:[[:space:]]+etcd[[:space:]]*$/ {
    component = "etcd"
  }

  is_statefulset && /^[[:space:]]+app:[[:space:]]+minio[[:space:]]*$/ {
    component = "minio"
  }

  is_statefulset && /^  volumeClaimTemplates:[[:space:]]*$/ {
    in_claim_templates = 1
  }

  {
    print
    if (in_claim_templates && component != "" && /^    - metadata:[[:space:]]*$/) {
      print "        labels:"
      print "          app.kubernetes.io/instance: \"" release "\""
      print "          app.kubernetes.io/name: \"" component "\""
      print "          daedalus.nvidia.com/milvus-storage: \"true\""
    }
  }
'
