#!/usr/bin/env python3
"""Exercise the extracted release with the documented config and libvirt test driver."""
import http.cookiejar
import os
import pathlib
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request
import zipfile


def smoke(archive):
    archive = pathlib.Path(archive).resolve()
    with tempfile.TemporaryDirectory() as tmp:
        if archive.is_dir():
            shutil.copytree(archive, tmp, dirs_exist_ok=True)
        elif archive.suffix == ".zip":
            with zipfile.ZipFile(archive) as source:
                source.extractall(tmp)
        else:
            with tarfile.open(archive) as source:
                source.extractall(tmp, filter="data")
        directory, = pathlib.Path(tmp).iterdir()
        binary = directory / ("vmango.exe" if os.name == "nt" else "vmango")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        config = (directory / "vmango.quickstart.conf").read_text()
        config = config.replace("qemu:///system", "test:///default")
        config = config.replace("8080", str(port))
        config = config.replace("REPLACE_WITH_RANDOM_SECRET", "smoke-test-session-secret-32-bytes")
        # This test-only hash is the existing sample password "admin".
        config = config.replace("REPLACE_WITH_GENPW_OUTPUT", "$2a$10$igHQGROHntvl05AztpfMeONSBDUsEbZHxayc5DOPTKIFX50WrHURS")
        (directory / "vmango.conf").write_text(config)
        with (directory / "server.log").open("w+") as log:
            server = subprocess.Popen([str(binary), "web", "-c", "vmango.conf"], cwd=directory, stdout=log, stderr=log)
            try:
                base = f"http://127.0.0.1:{port}"
                client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                for _ in range(100):
                    if server.poll() is not None:
                        raise RuntimeError("Server exited before becoming ready")
                    try:
                        client.open(base + "/login/", timeout=1).close()
                        break
                    except OSError:
                        time.sleep(0.1)
                else:
                    raise RuntimeError("Server did not become ready")
                credentials = urllib.parse.urlencode({"Username": "admin", "Password": "admin"}).encode()
                response = client.open(base + "/login/", credentials, timeout=10)
                if "/login" in response.url:
                    raise RuntimeError("Login failed")
                page = client.open(base + "/nodes/local/", timeout=10).read().decode()
                if "local" not in page or "error" in page.lower().split("<title>")[-1].split("</title>")[0]:
                    raise RuntimeError("Libvirt test node was not rendered")
                print("Archive smoke test passed: startup, login, libvirt test node")
            finally:
                server.terminate()
                server.wait(timeout=15)
                log.seek(0)
                print(log.read())


if __name__ == "__main__":
    smoke(sys.argv[1])
