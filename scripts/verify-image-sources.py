#!/usr/bin/env python3
"""Explicit maintainer verification. Stream artifacts without saving disk payloads.

Usage: python3 scripts/verify-image-sources.py [sources.json] [--record]
--record replaces metadata with newly verified SHA-256/header/HTTP observations.
It never edits the runtime configuration or follows upstream latest versions.
"""
import concurrent.futures
import datetime
import hashlib
import json
import pathlib
import struct
import sys
import urllib.request


def verify(source):
    source = dict(source)
    with urllib.request.urlopen(source["publisher_checksums"], timeout=60) as response:
        manifest = response.read(1024 * 1024).decode()
    if source["publisher_digest"] not in manifest.lower():
        raise ValueError("publisher checksum changed: " + source["id"])
    sha256 = hashlib.sha256()
    publisher = hashlib.new(source["publisher_algorithm"])
    count = 0
    prefix = bytearray()
    request = urllib.request.Request(source["url"], headers={"Accept-Encoding": "identity"})
    with urllib.request.urlopen(request, timeout=120) as response:
        if response.status != 200 or response.headers.get("Content-Encoding", "identity") != "identity":
            raise ValueError("unexpected artifact HTTP response")
        length = response.headers.get("Content-Length")
        content_type = response.headers.get("Content-Type")
        while True:
            chunk = response.read(128 * 1024)
            if not chunk:
                break
            if len(prefix) < 104:
                prefix.extend(chunk[:104 - len(prefix)])
            sha256.update(chunk)
            publisher.update(chunk)
            count += len(chunk)
            if count > 16 * 1024**3:
                raise ValueError("artifact exceeds verification limit")
    if publisher.hexdigest() != source["publisher_digest"]:
        raise ValueError("publisher checksum mismatch: " + source["id"])
    if length and count != int(length):
        raise ValueError("HTTP byte size mismatch")
    if len(prefix) < 104 or prefix[:4] != b"QFI\xfb":
        raise ValueError("artifact is not QCOW2: " + source["id"])
    if struct.unpack_from(">Q", prefix, 8)[0] or struct.unpack_from(">I", prefix, 16)[0]:
        raise ValueError("QCOW2 has a backing file")
    if source.get("sha256") and source["sha256"] != sha256.hexdigest():
        raise ValueError("pinned SHA-256 mismatch: " + source["id"])
    source.update(sha256=sha256.hexdigest(), size_bytes=count, format="qcow2",
                  virtual_size_bytes=struct.unpack_from(">Q", prefix, 24)[0],
                  incompatible_features=struct.unpack_from(">Q", prefix, 72)[0],
                  content_length=int(length) if length else None, content_type=content_type,
                  verified_at=datetime.datetime.now(datetime.timezone.utc).isoformat())
    print(source["id"], source["sha256"], count, "bytes", flush=True)
    return source


def main():
    path = pathlib.Path(next((arg for arg in sys.argv[1:] if not arg.startswith("--")),
                             "docs/data/image-sources.json"))
    sources = json.loads(path.read_text())
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        verified = list(executor.map(verify, sources))
    if "--record" in sys.argv:
        path.write_text(json.dumps(verified, indent=2) + "\n")


if __name__ == "__main__":
    main()
