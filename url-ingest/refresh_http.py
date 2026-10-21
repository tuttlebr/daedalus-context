"""Pace requests and preserve host backoff across Kubernetes retry pods.

One collection lock owns this policy directory. Threads share the governor;
requests to one host are serial and spaced even when cached pages are fast.
"""
from __future__ import annotations

import json
import math
import threading
import time
from email.utils import parsedate_to_datetime
from pathlib import Path
from urllib.parse import urlparse

import requests

from refresh_state import atomic_json, digest


def retry_after_seconds(value: str, now: float) -> float:
    try:
        seconds = float(value)
        return max(0, seconds) if math.isfinite(seconds) else 0
    except (TypeError, ValueError):
        try:
            return max(0, parsedate_to_datetime(value).timestamp() - now)
        except (TypeError, ValueError, OverflowError):
            return 0


class HostGovernor:
    def __init__(self, root: Path, interval: float = 0.5, *, clock=time.time, sleep=time.sleep):
        if interval < 0:
            raise ValueError('HTTP interval cannot be negative')
        self.root, self.interval, self.clock, self.sleep = root, interval, clock, sleep
        self._guard = threading.Lock()
        self._hosts = {}

    def get(self, client: requests.Session, url: str, **kwargs):
        host = (urlparse(url).hostname or '').lower()
        with self._guard:
            if host not in self._hosts:
                path = self.root / (digest(host) + '.json')
                state = json.loads(path.read_text()) if path.exists() else {}
                self._hosts[host] = (threading.Lock(), path, state)
            lock, path, state = self._hosts[host]
        with lock:
            # No other request can race a response that opens the circuit.
            delay = max(0, state.get('not_before', 0) - self.clock())
            if delay > 5:
                print(json.dumps({'event': 'http_backoff', 'host': host,
                                  'retry_at': state['not_before'], 'seconds': round(delay)}), flush=True)
            if delay:
                self.sleep(delay)
            response = client.get(url, **kwargs)
            now = self.clock()
            interval = max(self.interval, state.get('minimum_interval', 0))
            state['not_before'] = now + interval
            if response.status_code in (403, 429, 503):
                state['blocked_responses'] = state.get('blocked_responses', 0) + 1
                # Isolated permission failures need not stop a whole domain.
                # Repeated denials and explicit throttling open the circuit.
                if response.status_code != 403 or state['blocked_responses'] >= 3:
                    state['minimum_interval'] = min(30, max(1, interval * 2))
                    backoff = min(1800, 300 * 2 ** min(state['blocked_responses'] - 1, 3))
                    backoff = max(backoff, retry_after_seconds(response.headers.get('Retry-After'), now))
                    state['not_before'] = now + backoff
                atomic_json(path, state)
            elif state.get('blocked_responses'):
                state['blocked_responses'] = 0
                atomic_json(path, state)
            return response


class PacedSession(requests.Session):
    def __init__(self, governor: HostGovernor):
        super().__init__()
        self.governor = governor

    def get(self, url, **kwargs):
        # Give the governor the base implementation to avoid recursing here.
        class Transport:
            def get(_, url, **options):
                return super(PacedSession, self).get(url, **options)
        return self.governor.get(Transport(), url, **kwargs)
