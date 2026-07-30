from __future__ import annotations

import pathlib
import unittest

import yaml


MANIFEST = pathlib.Path(__file__).parents[1] / "k8s" / "publish-corpus-job.yaml"


class PublisherManifestTests(unittest.TestCase):
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
