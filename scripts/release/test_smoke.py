"""Keep release runtime probes aligned with the public API contract."""
import ast
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class SmokeAPIContractTest(unittest.TestCase):
    def test_runtime_probe_routes_exist_in_openapi(self):
        paths = json.loads((ROOT / "api/testdata/openapi.json").read_text())["paths"]
        routes = [re.compile(re.sub(r"\{[^}]+\}", "[^/]+", path)) for path in paths]
        for filename in ("smoke.py", "docker-smoke.py"):
            with self.subTest(script=filename):
                tree = ast.parse((ROOT / "scripts/release" / filename).read_text())
                probes = {
                    node.value for node in ast.walk(tree)
                    if isinstance(node, ast.Constant) and isinstance(node.value, str)
                    and node.value.startswith("/api/v1/")
                }
                self.assertTrue(probes, "No API probes found")
                for path in probes:
                    with self.subTest(path=path):
                        self.assertTrue(any(route.fullmatch(path) for route in routes),
                                        f"Runtime probe uses an undocumented API route: {path}")


if __name__ == "__main__":
    unittest.main()
