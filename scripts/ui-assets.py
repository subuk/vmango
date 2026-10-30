#!/usr/bin/env python3
"""Bind a frontend artifact to its sources and verify it without Node/npm."""
import hashlib
import json
import pathlib
import os
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
UI = ROOT / "ui"
DIST = UI / "dist"
MANIFEST = DIST / "build-manifest.json"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inputs():
    files = [ROOT / "api/testdata/openapi.json", ROOT / "scripts/ui-assets.py"]
    for directory, names, filenames in os.walk(UI):
        names[:] = [name for name in names if name not in ("node_modules", "dist", ".git")]
        files += [pathlib.Path(directory) / name for name in filenames
                  if not name.endswith(".tsbuildinfo")]
    return {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}


def outputs():
    return {str(p.relative_to(DIST)): sha(p) for p in sorted(DIST.rglob("*"))
            if p.is_file() and p != MANIFEST}


def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "verify"
    if not (DIST / "index.html").is_file():
        raise ValueError("Frontend artifact missing: run make ui before building Go")
    expected = {"version": 1, "sources": inputs(), "assets": outputs()}
    if mode == "write":
        MANIFEST.write_text(json.dumps(expected, sort_keys=True, indent=2) + "\n")
    elif mode == "verify":
        actual = json.loads(MANIFEST.read_text())
        if actual != expected:
            raise ValueError("Frontend artifact is stale or damaged: run make ui")
    else:
        raise ValueError("Expected write or verify")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError) as exc:
        print(f"UI build verification failed: {exc}", file=sys.stderr)
        sys.exit(1)
