import importlib.util
import io
import json
from pathlib import Path
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    'retriever_network', Path(__file__).parents[1] / 'check_retriever_network.py')
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)
FORWARDING = 'kind: CiliumNetworkPolicy\nspec:\n  ingress:\n  - fromEntities: [world]\n'


class NetworkPrerequisiteTests(unittest.TestCase):
    def test_addressless_class_selects_deployment_readiness(self):
        service = 'kind: Service\nspec:\n  loadBalancerClass: daedalus.ai/nodeport-only\n'
        output = io.StringIO()
        with patch('sys.stdin', io.StringIO(service)), redirect_stdout(output):
            guard.main()
        self.assertEqual(output.getvalue().strip(), 'nodeport-only')

    def test_unfiltered_forwarding_fails_before_deployment(self):
        for value in ['false', None]:
            result = type('Result', (), {'stdout': json.dumps(
                {'data': {'bpf-lb-source-range-all-types': value}})})()
            with patch('sys.stdin', io.StringIO(FORWARDING)), \
                    patch.object(guard.subprocess, 'run', return_value=result), \
                    self.assertRaises(SystemExit):
                guard.main()

    def test_verified_filter_and_internal_only_profiles(self):
        result = type('Result', (), {'stdout': json.dumps(
            {'data': {'bpf-lb-source-range-all-types': 'true'}})})()
        with patch('sys.stdin', io.StringIO(FORWARDING)), \
                patch.object(guard.subprocess, 'run', return_value=result):
            guard.main()
        with patch('sys.stdin', io.StringIO('kind: Service\n')), \
                patch.object(guard.subprocess, 'run') as command:
            guard.main()
            command.assert_not_called()
