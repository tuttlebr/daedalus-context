import contextlib
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import requests

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from refresh import download_source, extract_batch, run, source_priority
from refresh_fetch import SourceRemoved
from refresh_http import HostGovernor, retry_after_seconds
from refresh_monitor import status
from refresh_state import State, RunReport, atomic_json, collection_lock, contract_fingerprint


def contract():
    return {'schema_version': 'g', 'generation': 'g', 'milvus_database': 'g',
            'active_database': 'default', 'embedding': {'dimension': 2}, 'chunking': {},
            'collections': {'c': {'urls': [], 'freshness_maximum_age_hours': 36,
                                  'http_min_interval_seconds': 0}}}


def response(code, headers=None):
    r = requests.Response()
    r.status_code, r.headers, r._content = code, headers or {}, b'content'
    r.url = 'https://example.com/a'
    return r


class Clock:
    def __init__(self):
        self.now, self.waits = 1000, []

    def time(self):
        return self.now

    def sleep(self, seconds):
        self.waits.append(seconds)
        self.now += seconds


class RetryTests(unittest.TestCase):
    def test_large_pdfs_get_a_longer_bounded_result_timeout(self):
        for kind, expected in [('pdf', 600), ('url', 60)]:
            builder = Mock()
            builder.files.return_value = builder
            builder.extract.return_value = builder
            builder.__enter__ = Mock(return_value=builder)
            builder.__exit__ = Mock(return_value=False)
            builder.ingest.return_value = ([], [])
            old = Mock()
            with patch.dict(sys.modules, {'nv_ingest_client.client': Mock(Ingestor=Mock(return_value=builder))}), \
                 patch('refresh.legacy_client', return_value=old), \
                 patch('refresh_embedding.embed_records', return_value=[]):
                extract_batch(['/test/document'], contract(), kind)
            self.assertEqual(old.BoundedNvIngestClient.call_args.kwargs['request_timeout_seconds'], expected)
            self.assertEqual(builder.ingest.call_args.kwargs['timeout'], expected)

    def test_failure_is_visible_before_next_source_completes(self):
        with tempfile.TemporaryDirectory() as tmp:
            report = RunReport(Path(tmp), 'c', 'fingerprint', 'run')
            report.fail('https://example.com/blocked', RuntimeError('HTTP 403'))
            self.assertEqual(json.loads(report.latest.read_text())['failure_count'], 1)

    def test_unfinished_sources_precede_new_sources_without_revisiting_successes(self):
        documents = {
            'failed': {'last_attempt': 10, 'last_error': 'HTTPError', 'sha256': 'h', 'ingested_hash': 'h'},
            'interrupted': {'last_attempt': 20, 'sha256': 'new', 'ingested_hash': 'old'},
            'good': {'last_attempt': 1, 'sha256': 'h', 'ingested_hash': 'h'},
            'missing': {'last_attempt': 1, 'missing_since': 90},
        }
        items = [(name, {}) for name in ('new', *documents)]
        ordered = sorted(items, key=lambda item: source_priority(item, documents, 100, 48))
        self.assertEqual([x[0] for x in ordered[:3]], ['failed', 'interrupted', 'new'])
        documents['missing']['missing_since'] = 1
        ordered = sorted(items, key=lambda item: source_priority(item, documents, 100, 48))
        self.assertEqual(ordered[0][0], 'missing')

    def test_expired_work_budget_checkpoints_without_advancing_freshness(self):
        with tempfile.TemporaryDirectory() as tmp:
            root, cfg = Path(tmp), contract()
            state = State(root / 'g/collections', 'c', contract_fingerprint(cfg))
            state.data['last_success'] = 100
            state.checkpoint()
            client = Mock()
            client.list_databases.return_value = ['default']
            client.query.return_value = [{'count(*)': 3}]
            module = Mock(MilvusClient=Mock(return_value=client))
            with patch.dict(sys.modules, {'pymilvus': module}), \
                 patch.dict(os.environ, {'MILVUS_URI': 'http://unused', 'MILVUS_USERNAME': 'u', 'MILVUS_PASSWORD': 'p'}), \
                 patch('refresh.Store'), \
                 patch('refresh.discover', return_value=({'https://example.com/a': {}}, [])), \
                 patch('refresh.download_source') as download:
                result = run(cfg, 'c', root, root / 'cache', root / 'pdf', budget_seconds=0)
            self.assertEqual(result['state'], 'partial')
            self.assertTrue(result['budget_exhausted'])
            self.assertEqual(result['last_success'], 100)
            download.assert_not_called()
            self.assertEqual(State(root / 'g/collections', 'c', contract_fingerprint(cfg)).data['last_success'], 100)

    def test_overlap_preserves_owner_report_and_freshness(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            active = {'state': 'running', 'run_id': 'owner', 'last_success': 123}
            atomic_json(root / 'g/status/c.json', active)
            atomic_json(root / 'g/heartbeats/c.json', {'at': 100, 'pod': 'owner'})
            with collection_lock(root / 'g/locks', 'c'), patch.dict(os.environ, {'POD_NAME': 'duplicate'}):
                result = run(contract(), 'c', root, root / 'cache', root / 'pdfs')
            self.assertEqual(result['state'], 'skipped_active')
            self.assertEqual(json.loads((root / 'g/status/c.json').read_text()), active)
            self.assertEqual(json.loads((root / 'g/heartbeats/c.json').read_text())['pod'], 'owner')
            self.assertTrue((root / 'g/runs/c/duplicate.json').exists())

    def test_retry_reuses_own_successful_check_but_next_job_revalidates(self):
        with tempfile.TemporaryDirectory() as tmp:
            cached = Path(tmp) / 'a.html'
            cached.write_text('original')
            previous = {'cache_path': str(cached), 'last_checked_job': 'job-one', 'sha256': 'hash'}
            http = Mock()
            http.__enter__ = Mock(return_value=http)
            http.__exit__ = Mock(return_value=False)
            item = ('https://example.com/a', {})
            with patch('refresh.session', return_value=http) as create, patch('refresh.fetch', return_value=(cached, previous)) as fetch:
                first = download_source(item, previous, Path(tmp), Mock(), 'job-one')
                self.assertTrue(first[2]['_reused_check'])
                create.assert_not_called()
                next_day = download_source(item, previous, Path(tmp), Mock(), 'job-two')
                fetch.assert_called_once()
                self.assertEqual(next_day[2]['last_checked_job'], 'job-two')

    def test_missing_cache_does_not_reuse_check(self):
        previous = {'cache_path': '/nonexistent/file', 'last_checked_job': 'job'}
        with patch('refresh.session') as create, patch('refresh.fetch', side_effect=RuntimeError('fetch attempted')):
            result = download_source(('https://example.com/a', {}), previous, Path('/unused'), Mock(), 'job')
            create.assert_called_once()
            self.assertIsInstance(result[3], RuntimeError)

    def test_missing_source_waits_for_grace_without_job_retry_or_deletion(self):
        with tempfile.TemporaryDirectory() as tmp:
            root, cfg = Path(tmp), contract()
            state = State(root / 'g/collections', 'c', contract_fingerprint(cfg))
            state.data['last_success'] = 100
            state.record('https://example.com/a', {'ingested_hash': 'old'})
            state.checkpoint()
            client = Mock()
            client.list_databases.return_value = ['default']
            client.query.return_value = [{'count(*)': 3}]
            store = Mock()
            module = Mock(MilvusClient=Mock(return_value=client))
            with patch.dict(sys.modules, {'pymilvus': module}), \
                 patch.dict(os.environ, {'MILVUS_URI': 'http://unused', 'MILVUS_USERNAME': 'u', 'MILVUS_PASSWORD': 'p'}), \
                 patch('refresh.Store', return_value=store), \
                 patch('refresh.discover', return_value=({'https://example.com/a': {}}, [])), \
                 patch('refresh.download_source', side_effect=lambda item, previous, *args: (item[0], None, previous, SourceRemoved('HTTP 404'))):
                result = run(cfg, 'c', root, root / 'cache', root / 'pdf')
                self.assertEqual(result['state'], 'pending_removals')
                self.assertEqual(result['failure_count'], 0)
                self.assertEqual(result['pending_removals'], 1)
                self.assertEqual(result['last_success'], 100)
                store.delete.assert_not_called()
                state = State(root / 'g/collections', 'c', contract_fingerprint(cfg))
                self.assertTrue(state.data['documents']['https://example.com/a']['missing_since'])
                self.assertFalse(status(cfg, root)['healthy'])
                state.record('https://example.com/a', {'ingested_hash': 'old', 'missing_since': 1})
                state.checkpoint()
                result = run(cfg, 'c', root, root / 'cache', root / 'pdf')
                self.assertEqual(result['state'], 'complete')
                store.delete.assert_called_once_with('https://example.com/a')


class GovernorTests(unittest.TestCase):
    def test_fast_cached_responses_are_paced(self):
        with tempfile.TemporaryDirectory() as tmp:
            clock = Clock()
            governor = HostGovernor(Path(tmp), 2, clock=clock.time, sleep=clock.sleep)
            client = Mock()
            client.get.return_value = response(304)
            governor.get(client, 'https://example.com/a')
            governor.get(client, 'https://example.com/b')
            governor.get(client, 'https://other.example/a')
            self.assertEqual(clock.waits, [2])

    def test_retry_after_survives_worker_restart(self):
        with tempfile.TemporaryDirectory() as tmp:
            clock, client = Clock(), Mock()
            client.get.return_value = response(429, {'Retry-After': '3600'})
            HostGovernor(Path(tmp), clock=clock.time, sleep=clock.sleep).get(client, 'https://example.com/a')
            client.get.return_value = response(200)
            HostGovernor(Path(tmp), clock=clock.time, sleep=clock.sleep).get(client, 'https://example.com/b')
            self.assertEqual(clock.waits, [3600])

    def test_blocked_host_retains_slower_pacing_after_recovery(self):
        with tempfile.TemporaryDirectory() as tmp:
            clock, client = Clock(), Mock()
            client.get.return_value = response(429)
            HostGovernor(Path(tmp), 5, clock=clock.time, sleep=clock.sleep).get(client, 'https://example.com/a')
            client.get.return_value = response(200)
            recovered = HostGovernor(Path(tmp), 5, clock=clock.time, sleep=clock.sleep)
            recovered.get(client, 'https://example.com/b')
            recovered.get(client, 'https://example.com/c')
            self.assertEqual(clock.waits, [300, 10])

    def test_repeated_forbidden_responses_open_host_circuit(self):
        with tempfile.TemporaryDirectory() as tmp:
            clock, client = Clock(), Mock()
            governor = HostGovernor(Path(tmp), 0, clock=clock.time, sleep=clock.sleep)
            client.get.return_value = response(403)
            for i in range(3):
                governor.get(client, f'https://example.com/{i}')
            client.get.return_value = response(200)
            governor.get(client, 'https://example.com/recovered')
            self.assertEqual(clock.waits, [1200])

    def test_retry_after_date_and_bad_header(self):
        self.assertEqual(retry_after_seconds('Thu, 01 Jan 1970 01:00:00 GMT', 1000), 2600)
        self.assertEqual(retry_after_seconds('invalid', 1000), 0)
        self.assertEqual(retry_after_seconds('inf', 1000), 0)

    def test_waiting_host_does_not_mask_dead_worker(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            atomic_json(root / 'g/status/c.json', {'state': 'running', 'last_success': 100, 'updated_at': 100})
            atomic_json(root / 'g/heartbeats/c.json', {'at': 5000})
            atomic_json(root / 'g/collections/c/http-hosts/host.json', {'not_before': 10000, 'blocked_responses': 3})
            self.assertFalse(status(contract(), root, now=5000)['collections']['c']['stalled'])
            self.assertTrue(status(contract(), root, now=5500)['collections']['c']['stalled'])


if __name__ == '__main__':
    unittest.main()
