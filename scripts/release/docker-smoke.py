#!/usr/bin/env python3
"""Check the final Docker runtime without touching the host's libvirt or storage."""
import json
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, timeout=60).strip()


def smoke(image, version, architecture=None):
    name = "vmango-smoke-" + uuid.uuid4().hex
    root = pathlib.Path(__file__).resolve().parents[2]
    config = (root / "vmango.quickstart.conf").read_text()
    config = config.replace('listen = "127.0.0.1:8080"', 'listen = ":8080"')
    # Docker forwards a non-loopback peer: use the configuration for a trusted
    # HTTPS reverse proxy. The test talks directly to its private HTTP backend.
    config = config.replace('base_url = "http://localhost:8080"',
                            'base_url = "https://localhost"\n    session_secure = true')
    config = config.replace("qemu:///system", "test:///default")
    config = config.replace("~/.vmango/", "/var/lib/vmango/")
    # This test-only hash uses the password "admin"; it never enters the image.
    config = config.replace("REPLACE_WITH_GENPW_OUTPUT", "$2a$10$igHQGROHntvl05AztpfMeONSBDUsEbZHxayc5DOPTKIFX50WrHURS")
    metadata, = json.loads(docker("image", "inspect", image))
    if architecture and (metadata["Os"], metadata["Architecture"]) != ("linux", architecture):
        raise RuntimeError("Image platform differs from the expected Linux architecture")
    if metadata["Config"]["User"] != "10001:10001":
        raise RuntimeError("Container must run as the unprivileged Vmango user")
    for command in ("ssh", "genisoimage", "virsh"):
        docker("run", "--rm", "--entrypoint", "/bin/sh", image, "-c", f"command -v {command}")
    docker("volume", "create", name)
    try:
        docker("create", "--name", name, "--publish", "127.0.0.1::8080",
               "--volume", name + ":/var/lib/vmango", image)
        with tempfile.TemporaryDirectory() as directory:
            filename = pathlib.Path(directory) / "vmango.conf"
            filename.write_text(config)
            docker("cp", str(filename), name + ":/etc/vmango.conf")
        docker("start", name)
        def check():
            # Docker can assign a new ephemeral host port after a restart.
            port = docker("port", name, "8080/tcp").rsplit(":", 1)[1]
            base = "http://127.0.0.1:" + port
            for _ in range(100):
                if docker("inspect", "--format", "{{.State.Running}}", name) != "true":
                    raise RuntimeError("Container exited before becoming ready")
                try:
                    urllib.request.urlopen(base + "/login/", timeout=1).close()
                    break
                except OSError:
                    time.sleep(0.1)
            else:
                raise RuntimeError("Container did not become ready")
            credentials = json.dumps({"username": "admin", "password": "admin"}).encode()
            request = urllib.request.Request(base + "/api/v1/auth/login", credentials,
                                             {"Content-Type": "application/json"})
            with urllib.request.urlopen(request, timeout=10) as response:
                token = json.load(response)["access_token"]

            def get(path):
                request = urllib.request.Request(base + path, headers={"Authorization": "Bearer " + token})
                with urllib.request.urlopen(request, timeout=10) as response:
                    return json.load(response)

            if get("/api/v1/hosts/local")["id"] != "local":
                raise RuntimeError("Libvirt test node was not returned")
            if get("/api/v1/me")["version"] != version:
                raise RuntimeError("Embedded application version differs from the image version")
            with urllib.request.urlopen(base + "/machines/", timeout=10) as response:
                assets = re.findall(r'(?:src|href)="(/assets/[^"\s]+)"', response.read().decode())
            if not assets:
                raise RuntimeError("Embedded frontend assets missing")
            for asset in assets:
                with urllib.request.urlopen(base + asset, timeout=10) as response:
                    if not response.read():
                        raise RuntimeError("Empty embedded frontend asset")

        check()
        key = docker("exec", name, "cat", "/var/lib/vmango/jwt_signing_key")
        docker("restart", name)
        check()
        if docker("exec", name, "cat", "/var/lib/vmango/jwt_signing_key") != key:
            raise RuntimeError("Signing key changed after restart")
        print("Docker smoke test passed: JWT login, embedded UI/version, libvirt test node, persistent state")
    finally:
        subprocess.run(["docker", "logs", name], check=False, timeout=30)
        subprocess.run(["docker", "rm", "--force", name], check=False, timeout=30)
        docker("volume", "rm", name)


if __name__ == "__main__":
    smoke(*sys.argv[1:])
