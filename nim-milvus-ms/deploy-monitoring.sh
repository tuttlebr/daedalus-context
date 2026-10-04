#!/usr/bin/env bash
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXPECTED_CONTEXT=${EXPECTED_CONTEXT:?Set EXPECTED_CONTEXT to the approved literal Kubernetes context}
HELM_TIMEOUT=${HELM_TIMEOUT:-5m}

if [[ "$(kubectl config current-context)" != "$EXPECTED_CONTEXT" ]]; then
  echo "Refusing to upgrade a different Kubernetes context." >&2
  exit 1
fi
python3 -c 'import yaml'

python3 - "$EXPECTED_CONTEXT" <<'PY_OWNER'
import json
import subprocess
import sys

obj = json.loads(subprocess.check_output([
    "kubectl", "--context", sys.argv[1], "-n", "monitoring", "get",
    "prometheus", "kube-prometheus-maintenance", "-o", "json",
]))
annotations = obj["metadata"].get("annotations", {})
if (
    annotations.get("meta.helm.sh/release-name") not in (None, "kube-prometheus-stack")
    or annotations.get("meta.helm.sh/release-namespace") not in (None, "monitoring")
):
    raise SystemExit("Active Prometheus belongs to a different Helm release.")
PY_OWNER

# Capture the current user settings privately so unrelated settings survive.
# Resetting chart defaults avoids --reuse-values restoring a removed hostname.
task_values_file=$(mktemp)
trap 'rm -f "$task_values_file"' EXIT
helm --kube-context "$EXPECTED_CONTEXT" -n monitoring get values \
  kube-prometheus-stack -o yaml > "$task_values_file"

helm --kube-context "$EXPECTED_CONTEXT" -n monitoring upgrade \
  kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  --version 82.18.0 \
  --reset-values \
  --values "$task_values_file" \
  --values "$SCRIPT_DIR/kube-prometheus-stack-values.yaml" \
  --take-ownership \
  --wait \
  --timeout "$HELM_TIMEOUT"

python3 - "$EXPECTED_CONTEXT" <<'PY'
import json
import subprocess
import sys
import time
import yaml

context = sys.argv[1]
kubectl = ["kubectl", "--context", context, "-n", "monitoring"]
manifest = subprocess.check_output(
    ["helm", "--kube-context", context, "-n", "monitoring", "get", "manifest",
     "kube-prometheus-stack"], text=True,
)
desired = next(
    obj for obj in yaml.safe_load_all(manifest)
    if obj and obj["kind"] == "Prometheus"
    and obj["metadata"]["name"] == "kube-prometheus-maintenance"
)
actual = json.loads(subprocess.check_output(
    [*kubectl, "get", "prometheus", "kube-prometheus-maintenance", "-o", "json"],
))
owner = actual["metadata"].get("annotations", {}).get("meta.helm.sh/release-name")
if owner not in (None, "kube-prometheus-stack"):
    raise SystemExit("Active Prometheus belongs to a different Helm release.")

# Helm 3.17 can record an adopted resource without updating its live fields.
# Align the hostname and ownership explicitly with the stored Helm manifest.
if {k: v for k, v in actual["spec"].items() if k != "nodeSelector"} != desired["spec"]:
    raise SystemExit("Active Prometheus differs beyond the hostname selector.")
labels = {**actual["metadata"].get("labels", {}), **desired["metadata"]["labels"]}
annotations = {
    **actual["metadata"].get("annotations", {}),
    **desired["metadata"]["annotations"],
    "meta.helm.sh/release-name": "kube-prometheus-stack",
    "meta.helm.sh/release-namespace": "monitoring",
}
patch = [
    {"op": "test", "path": "/metadata/uid", "value": actual["metadata"]["uid"]},
    {"op": "test", "path": "/metadata/resourceVersion",
     "value": actual["metadata"]["resourceVersion"]},
]
if "nodeSelector" in actual["spec"]:
    patch.append({"op": "remove", "path": "/spec/nodeSelector"})
if labels != actual["metadata"].get("labels", {}):
    patch.append({"op": "add", "path": "/metadata/labels", "value": labels})
if annotations != actual["metadata"].get("annotations", {}):
    patch.append({"op": "add", "path": "/metadata/annotations", "value": annotations})
if len(patch) > 2:
    subprocess.run(
        [*kubectl, "patch", "prometheus", "kube-prometheus-maintenance",
         "--type=json", "-p", json.dumps(patch)], check=True,
    )

command = [
    "kubectl", "--context", sys.argv[1], "-n", "monitoring", "get",
    "statefulset", "prometheus-kube-prometheus-maintenance", "-o", "json",
]
deadline = time.monotonic() + 300
while time.monotonic() < deadline:
    obj = json.loads(subprocess.check_output(command))
    status = obj.get("status", {})
    if (
        not obj["spec"]["template"]["spec"].get("nodeSelector")
        and status.get("observedGeneration") == obj["metadata"]["generation"]
        and status.get("readyReplicas") == obj["spec"]["replicas"] == 1
        and status.get("currentRevision") == status.get("updateRevision")
    ):
        print("Active Prometheus is ready without a hostname selector.")
        break
    time.sleep(2)
else:
    raise SystemExit("Prometheus reconciliation did not finish within 5 minutes.")
PY
