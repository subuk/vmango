"""Check that native archives contain usable offline documentation."""
import importlib.util
from pathlib import Path
import re
import shutil
import tarfile
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("release_package", ROOT / "scripts/release/package.py")
PACKAGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PACKAGE)


class OfflineDocumentationTest(unittest.TestCase):
    def test_archive_includes_guides_and_resolves_guide_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for filename in (
                "LICENSE", "vmango.quickstart.conf", "vmango.dist.conf", "docs/API.md",
                "docs/CLOUDS.md", "docs/TASKS.md", "docs/UI-MIGRATION.md",
                "docs/content/en/docs/start.md", "docs/content/en/docs/images.md",
                "docs/content/en/docs/docker.md",
                "docs/data/image-sources.json",
            ):
                target = root / filename
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / filename, target)
            # Packaging only reads these inputs; executable behavior is outside this test.
            binary = root / "vmango"
            binary.write_bytes(b"packaging fixture")
            notices = root / "ui/dist/THIRD-PARTY-LICENSES.txt"
            notices.parent.mkdir(parents=True)
            notices.write_text("packaging fixture")
            with mock.patch.object(PACKAGE, "__file__", str(root / "scripts/release/package.py")):
                archive = PACKAGE.package("test", "linux-x64", binary)
            with tarfile.open(archive) as packaged:
                prefix = "vmango-test-linux-x64/"
                files = {item.name.removeprefix(prefix) for item in packaged if item.isfile()}
                for guide in ("API.md", "CLOUDS.md", "DOCKER.md", "IMAGES.md", "MIGRATION.md", "TASKS.md", "QUICKSTART.md"):
                    self.assertIn(guide, files)
                for guide in ("API.md", "CLOUDS.md", "DOCKER.md", "IMAGES.md", "MIGRATION.md", "TASKS.md", "QUICKSTART.md"):
                    text = packaged.extractfile(prefix + guide).read().decode()
                    for target in re.findall(r"\]\(([^)]+)\)", text):
                        if "://" not in target and not target.startswith("#"):
                            self.assertIn(target.split("#", 1)[0], files, (guide, target))
                self.assertIn("](UI-MIGRATION.md)", (root / "docs/API.md").read_text())


if __name__ == "__main__":
    unittest.main()
