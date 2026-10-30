from __future__ import annotations

import pathlib
import os
import subprocess
import tempfile
import unittest

import yaml


MANIFEST = pathlib.Path(__file__).parents[1] / "k8s" / "publish-corpus-job.yaml"


class PublisherManifestTests(unittest.TestCase):
    def test_publication_only_creates_a_job_and_waits_for_it(self):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            kubectl = work / "kubectl"
            kubectl.write_text("""#!/usr/bin/env python3
import json, os, pathlib, sys
work = pathlib.Path(os.environ['PUBLISH_TEST_DIR'])
with (work / 'calls').open('a') as stream:
    stream.write(json.dumps(sys.argv[1:]) + '\\n')
if 'create' in sys.argv:
    manifest = pathlib.Path(sys.argv[sys.argv.index('-f') + 1])
    (work / 'job.yaml').write_text(manifest.read_text())
elif not any(command in sys.argv for command in ('get', 'wait')):
    sys.exit('unexpected cluster operation')
""")
            kubectl.chmod(0o755)
            subprocess.run([
                "bash", str(MANIFEST.parents[1] / "publish-corpus.sh"),
            ], check=True, env={**os.environ, "PATH": f"{work}:{os.environ['PATH']}",
                               "PUBLISH_TEST_DIR": str(work), "NAMESPACE": "daedalus",
                               "CORPUS_GENERATION": "test-generation",
                               "PDF_SOURCE_HOST_PATH": "/test/corpus", "PDF_SOURCE_NODE": "test-node"})
            document = yaml.safe_load((work / "job.yaml").read_text())
            self.assertEqual(document["kind"], "Job")
            self.assertEqual(document["metadata"]["namespace"], "daedalus")
            self.assertEqual(len((work / "calls").read_text().splitlines()), 3)

    def test_publication_uses_one_fenced_generation_pointer(self):
        document = yaml.safe_load(MANIFEST.read_text(encoding="utf-8"))
        script = document["spec"]["template"]["spec"]["containers"][0]["args"][0]

        self.assertIn('publish_lock="/destination/.publish-lock"', script)
        self.assertIn('test "${PUBLISH_FENCE}" -gt "${previous_fence}"', script)
        self.assertIn('ln -s "current/${collection}"', script)
        self.assertIn('ln -s ".generations/${CORPUS_GENERATION}"', script)
        self.assertEqual(script.count("mv -f \"${pointer}\" /destination/current"), 1)
        self.assertLess(
            script.index("legacy directory"),
            script.index('mv "${staging_root}" "${generation_root}"'),
        )


if __name__ == "__main__":
    unittest.main()
