"""Regression checks for offline source/asset verification."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


class UIManifestTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="vmango-manifest-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        for directory in ("ui/src", "ui/dist/assets", "api/testdata", "scripts"):
            (self.root / directory).mkdir(parents=True, exist_ok=True)
        shutil.copy2(Path(__file__).with_name("ui-assets.py"), self.root / "scripts/ui-assets.py")
        (self.root / "api/testdata/openapi.json").write_text('{}\n')
        (self.root / "ui/src/main.ts").write_text("console.log('application');\n")
        (self.root / "ui/dist/index.html").write_text('<script src="/assets/app.js"></script>\n')
        (self.root / "ui/dist/assets/app.js").write_text("console.log('application');\n")
        self.assertEqual(self.run_manifest("write").returncode, 0)

    def run_manifest(self, mode="verify"):
        # The verifier uses only Python, including in downstream package builds.
        return subprocess.run([sys.executable, str(self.root / "scripts/ui-assets.py"), mode],
                              cwd=self.root, env=dict(os.environ, PATH="/usr/bin:/bin"),
                              capture_output=True, text=True)

    def test_valid_artifact_needs_no_node(self):
        self.assertEqual(self.run_manifest().returncode, 0)

    def test_source_removal_and_rename_are_detected(self):
        source = self.root / "ui/src/main.ts"
        source.rename(source.with_name("renamed.ts"))
        self.assertNotEqual(self.run_manifest().returncode, 0)
        self.assertIn("make ui", self.run_manifest().stderr)

    def test_asset_damage_and_unlisted_files_are_detected(self):
        (self.root / "ui/dist/assets/app.js").write_text("stale build")
        self.assertNotEqual(self.run_manifest().returncode, 0)
        self.assertEqual(self.run_manifest("write").returncode, 0)
        (self.root / "ui/dist/assets/unlisted.js").write_text("unlisted")
        self.assertNotEqual(self.run_manifest().returncode, 0)

    def test_missing_index_is_not_replaced_with_a_dummy(self):
        (self.root / "ui/dist/index.html").unlink()
        result = self.run_manifest()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("run make ui", result.stderr)


if __name__ == "__main__":
    unittest.main()
