import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    'retriever_network', Path(__file__).parents[1] / 'check_retriever_network.py')
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)
FORWARDING = 'kind: CiliumNetworkPolicy\nspec:\n  ingress:\n  - fromEntities: [world]\n'


class NetworkPrerequisiteTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("helm"), "helm is required for chart rendering")
    def test_deploy_checks_rendered_source_filter_before_selecting_helm_wait(self):
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            helm = directory / "helm"
            helm.write_text(
                '#!/bin/sh\n'
                'case "$1" in\n'
                '  template) exec "$REAL_HELM" "$@" ;;\n'
                '  upgrade) printf "%s\\n" "$@" > "$UPGRADE_ARGS" ;;\n'
                '  *) exit 1 ;;\n'
                'esac\n'
            )
            kubectl = directory / "kubectl"
            kubectl.write_text(
                '#!/bin/sh\n'
                'case "$*" in\n'
                '  "config current-context") echo fixture ;;\n'
                '  *"get configmap cilium-config"*) '
                'printf \'{"data":{"bpf-lb-source-range-all-types":"%s"}}\\n\' "$FILTER" ;;\n'
                'esac\n'
            )
            helm.chmod(0o700)
            kubectl.chmod(0o700)
            args_file = directory / "upgrade"
            overlay = "nemotron-retriever/chart/values-daedalus.yaml"
            for values, source_filter, expected_wait in (
                (overlay, "false", None), (overlay, "true", False), ("", "false", True)
            ):
                with self.subTest(values=values, source_filter=source_filter):
                    args_file.unlink(missing_ok=True)
                    result = subprocess.run(
                        ["make", "deploy-retriever", "EXPECTED_CONTEXT=fixture",
                         f"HELM={helm}", f"KUBECTL={kubectl}", f"RETRIEVER_VALUES_FILE={values}"],
                        cwd=root, capture_output=True, text=True,
                        env={**os.environ, "REAL_HELM": shutil.which("helm"),
                             "UPGRADE_ARGS": str(args_file), "FILTER": source_filter},
                    )
                    if expected_wait is None:
                        self.assertNotEqual(result.returncode, 0)
                        self.assertIn("bpf-lb-source-range-all-types", result.stderr)
                        self.assertFalse(args_file.exists())
                    else:
                        self.assertEqual(result.returncode, 0, result.stderr)
                        args = args_file.read_text().splitlines()
                        self.assertEqual("--wait" in args, expected_wait)
                        self.assertEqual("--atomic" in args, expected_wait)

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
