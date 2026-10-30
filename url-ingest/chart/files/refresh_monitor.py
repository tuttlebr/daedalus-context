#!/usr/bin/env python3
"""Read durable refresh reports; freshness never follows pod readiness alone."""
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

CONTRACT = Path(os.getenv("REFRESH_CONTRACT", "/etc/refresh/contract.json"))
ROOT = Path(os.getenv("REFRESH_STATE_ROOT", "/manifests/refresh"))


def status(contract: dict, root: Path, now: float | None = None) -> dict:
    now = now or time.time()
    generation = root / contract["generation"]
    output = {}
    for name, config in contract["collections"].items():
        path = generation / "status" / f"{name}.json"
        try:
            report = json.loads(path.read_text()) if path.exists() else {"state": "never_run"}
            success = report.get("last_success", 0)
            maximum = config["freshness_maximum_age_hours"] * 3600
            report["fresh"] = bool(success and now - success <= maximum)
            report["age_seconds"] = max(0, now - success) if success else -1
            beat = generation / "heartbeats" / f"{name}.json"
            last_beat = json.loads(beat.read_text()).get("at", 0) if beat.exists() else 0
            report["heartbeat_age_seconds"] = max(0, now - last_beat) if last_beat else -1
            host_states = generation / 'collections' / name / 'http-hosts'
            policies = [json.loads(p.read_text()) for p in host_states.glob('*.json')]
            report['http_backoff_until'] = max((p.get('not_before', 0) for p in policies
                                               if p.get('blocked_responses')), default=0)
            report['waiting_http'] = report['http_backoff_until'] > now + 5
            report["stalled"] = report["state"] == "running" and (
                not last_beat or now - last_beat > 180 or
                (now - report.get("updated_at", 0) > 3600 and not report['waiting_http']))
            report["healthy"] = (report["fresh"] and not report["stalled"] and not report['waiting_http'] and
                                 report["state"] in {'complete', 'running'} and
                                 not report.get('failure_count') and not report.get('pending_removals'))
            report.pop("failures", None)  # Full errors remain in the immutable run journal.
            output[name] = report
        except (ValueError, OSError, KeyError) as error:
            output[name] = {"state": "invalid_report", "fresh": False, "healthy": False, "error": str(error)}
    return {"generation": contract["generation"], "healthy": all(x["healthy"] for x in output.values()), "collections": output}


def metrics(snapshot: dict) -> str:
    lines = []
    for name, report in snapshot["collections"].items():
        labels = f'collection="{name}",generation="{snapshot["generation"]}"'
        values = {
            "fresh": int(report["fresh"]), "healthy": int(report["healthy"]),
            "last_success_timestamp_seconds": report.get("last_success", 0),
            "failures": report.get("failure_count", 0),
            "pending_removals": report.get("pending_removals", 0),
            "http_backoff_until": report.get("http_backoff_until", 0),
            "stalled": int(report.get("stalled", False)),
            "checked": report.get("checked", 0), "updated": report.get("updated", 0),
            "last_progress_timestamp_seconds": report.get("updated_at", 0),
        }
        for key, value in values.items():
            lines.append(f'daedalus_refresh_{key}{{{labels}}} {value}')
    return "\n".join(lines) + "\n"


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/healthz":
            body, code, mime = b"ok\n", 200, "text/plain"
        elif self.path in {"/status", "/readyz", "/metrics"}:
            result = status(json.loads(CONTRACT.read_text()), ROOT)
            if self.path == "/metrics":
                body, code, mime = metrics(result).encode(), 200, "text/plain; version=0.0.4"
            else:
                body = json.dumps(result, sort_keys=True).encode()
                code = 503 if self.path == "/readyz" and not result["healthy"] else 200
                mime = "application/json"
        else:
            body, code, mime = b"not found\n", 404, "text/plain"
        self.send_response(code)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
