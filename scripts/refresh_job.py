#!/usr/bin/env python3
"""Start a resumable refresh from its deployed CronJob contract."""
import argparse
import json
import os
import subprocess
import time


def kubectl(*args):
    return subprocess.check_output([os.getenv("KUBECTL", "kubectl"), "-n", os.getenv("DAEDALUS_NAMESPACE", "daedalus"), *args], text=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--collection", required=True)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--name")
    parser.add_argument("--verify", action="store_true")
    args = parser.parse_args()
    name = args.name or f"refresh-{args.collection}-{int(time.time())}"
    job = json.loads(kubectl("create", "job", name, f"--from=cronjob/url-ingest-{args.collection}", "--dry-run=client", "-o", "json"))
    if args.verify:
        container = job["spec"]["template"]["spec"]["containers"][0]
        container["command"] = ["/opt/nv_ingest_runtime/bin/python3", "/opt/daedalus/refresh_verify.py"]
        container["args"] = []
        job["spec"]["backoffLimit"] = 0
        job["spec"]["activeDeadlineSeconds"] = 600
    if args.limit:
        job["spec"]["backoffLimit"] = 0
        job["spec"]["activeDeadlineSeconds"] = 1800
        job["spec"]["template"]["spec"]["containers"][0]["args"].extend(["--limit", str(args.limit)])
    subprocess.run([os.getenv("KUBECTL", "kubectl"), "-n", os.getenv("DAEDALUS_NAMESPACE", "daedalus"), "create", "-f", "-"], input=json.dumps(job), text=True, check=True)


if __name__ == "__main__":
    main()
