#!/usr/bin/env python3
"""Exercise an installer-generated configuration through a running Vmango server.

The server must already be started with --config pointing at the supplied file.
This test creates one uniquely named VM through the API and removes only that
VM and its attached disks. Prepared host networks, pools and image caches stay.
Credentials are read from VMANGO_TEST_USER / VMANGO_TEST_PASSWORD, never argv.
No third-party Python packages are required.
"""

import argparse
import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import ssl
import struct
import sys
import time
from urllib.error import HTTPError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPCookieProcessor, Request, build_opener
import uuid


TERMINAL = {"succeeded", "failed", "cancelled", "interrupted"}


class API:
    def __init__(self, origin, namespace, password, username):
        self.origin = origin.rstrip("/")
        self.namespace = namespace
        self.password, self.username = password, username
        self.access = ""
        self.opener = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(self, method, path, data=None, status=200, retry=True):
        if not path.startswith("/api/v1/"):
            raise RuntimeError("Refusing an API URL outside this server")
        headers = {"Content-Type": "application/json", "Origin": self.origin,
                   "X-Vmango-Namespace": self.namespace}
        if self.access:
            headers["Authorization"] = "Bearer " + self.access
        if method != "GET" and "/clouds/" in path and "/consoles/" not in path:
            headers["Idempotency-Key"] = str(uuid.uuid4())
        body = None if data is None else json.dumps(data).encode()
        req = Request(self.origin + path, data=body, headers=headers, method=method)
        try:
            response = self.opener.open(req, timeout=120)
        except HTTPError as error:
            response = error
        with response:
            raw = response.read(2 << 20)
            code = response.code
        if code == 401 and retry and path != "/api/v1/auth/login":
            self.login()
            return self.request(method, path, data, status, retry=False)
        if code != status:
            raise RuntimeError(f"{method} {path}: HTTP {code}, expected {status}: {raw.decode(errors='replace')[:2000]}")
        return json.loads(raw) if raw else None

    def login(self):
        credentials = self.request("POST", "/api/v1/auth/login",
                                   {"username": self.username, "password": self.password}, retry=False)
        self.access = credentials["access_token"]


class WebSocket:
    """Bounded RFC 6455 binary reader for the real serial and VNC endpoints."""

    def __init__(self, api, path, timeout):
        target = urlsplit(api.origin)
        port = target.port or (443 if target.scheme == "https" else 80)
        self.socket = socket.create_connection((target.hostname, port), timeout=15)
        if target.scheme == "https":
            self.socket = ssl.create_default_context().wrap_socket(self.socket, server_hostname=target.hostname)
        self.socket.settimeout(timeout)
        self.reader = self.socket.makefile("rb")
        key = base64.b64encode(os.urandom(16)).decode()
        request = (f"GET {path} HTTP/1.1\r\nHost: {target.netloc}\r\n"
                   "Upgrade: websocket\r\nConnection: Upgrade\r\n"
                   f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n"
                   "Sec-WebSocket-Protocol: binary\r\n"
                   f"Origin: {api.origin}\r\nAuthorization: Bearer {api.access}\r\n"
                   f"X-Vmango-Namespace: {api.namespace}\r\n\r\n")
        self.socket.sendall(request.encode("ascii"))
        status = self.reader.readline(8193)
        if b" 101 " not in status:
            self.close()
            raise RuntimeError(f"Console upgrade failed: {status.decode(errors='replace').strip()}")
        headers = {}
        size = 0
        while True:
            line = self.reader.readline(8193)
            size += len(line)
            if size > 32768 or not line:
                self.close()
                raise RuntimeError("Invalid WebSocket handshake headers")
            if line == b"\r\n":
                break
            name, value = line.decode("ascii").split(":", 1)
            headers[name.lower()] = value.strip()
        expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        if headers.get("sec-websocket-accept") != expected:
            self.close()
            raise RuntimeError("Invalid WebSocket handshake proof")

    def exact(self, length):
        data = self.reader.read(length)
        if len(data) != length:
            raise RuntimeError("Console closed before verification completed")
        return data

    def send_control(self, opcode, data=b""):
        mask = os.urandom(4)
        self.socket.sendall(bytes([0x80 | opcode, 0x80 | len(data)]) + mask
                            + bytes(value ^ mask[index % 4] for index, value in enumerate(data)))

    def receive(self):
        while True:
            first, second = self.exact(2)
            opcode = first & 15
            size = second & 127
            if first & 0x70 or second & 0x80:
                raise RuntimeError("Unexpected WebSocket extension or masked server frame")
            if size == 126:
                size = struct.unpack("!H", self.exact(2))[0]
            elif size == 127:
                size = struct.unpack("!Q", self.exact(8))[0]
            if size > 16 << 20:
                raise RuntimeError("Console frame exceeded the test limit")
            data = self.exact(size)
            if opcode == 8:
                raise RuntimeError("Console closed before verification completed")
            if opcode == 9:
                if len(data) > 125:
                    raise RuntimeError("Oversized WebSocket ping")
                self.send_control(10, data)
            elif opcode in (0, 1, 2):
                return data
            elif opcode != 10:
                raise RuntimeError("Unexpected WebSocket opcode")

    def close(self):
        if hasattr(self, "reader"):
            self.reader.close()
        self.socket.close()


class Lifecycle:
    def __init__(self, args):
        self.args = args
        self.name = "vmango-installer-test-" + uuid.uuid4().hex[:12]
        self.root = Path(args.output or "result/" + self.name).absolute()
        self.root.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.api = API(args.url, args.namespace, os.environ["VMANGO_TEST_PASSWORD"],
                       os.environ.get("VMANGO_TEST_USER", "admin"))
        self.vm_id = None
        self.cloud = args.cloud
        self.create_task = None
        self.disks = []
        self.summary = {"name": self.name, "config_path": str(Path(args.config).absolute()),
                        "config_sha256": hashlib.sha256(Path(args.config).read_bytes()).hexdigest(),
                        "checks": [], "cleanup": "not started"}

    def record(self, filename, data):
        target = self.root / filename
        with target.open("w") as output:
            os.chmod(target, 0o600)
            json.dump(data, output, indent=2)
            output.write("\n")

    def check(self, message):
        self.summary["checks"].append(message)
        print("PASS:", message, flush=True)

    def task(self, method, path, data=None):
        accepted = self.api.request(method, path, data, status=202)
        task = accepted["task"]
        if task["kind"] == "machine.create":
            self.create_task = accepted["status_url"]
        deadline = time.monotonic() + self.args.task_timeout
        while True:
            task = self.api.request("GET", accepted["status_url"])
            if task["kind"] == "machine.create":
                ids = {ref["id"] for ref in task.get("resources", []) if ref["kind"] == "machine"}
                if len(ids) > 1:
                    raise RuntimeError("Create task reported multiple machine identities")
                if ids:
                    candidate = ids.pop()
                    uuid.UUID(candidate)
                    if self.vm_id and candidate != self.vm_id:
                        raise RuntimeError("Create task changed its machine identity")
                    self.vm_id = candidate
                    self.summary["vm_id"] = candidate
            if task["state"] in TERMINAL:
                self.record("task-" + task["id"] + ".json", task)
                if task["state"] != "succeeded":
                    raise RuntimeError(f"Task {task['id']} {task['kind']}: {task['state']}; see its saved task record")
                return task
            if time.monotonic() > deadline:
                self.record("task-" + task["id"] + ".json", task)
                raise RuntimeError(f"Task {task['id']} timed out; active work may still be running")
            time.sleep(0.5)

    def machine_path(self):
        return "/api/v1/clouds/" + quote(self.cloud, safe="") + "/machines/" + self.vm_id

    def run(self):
        self.api.login()
        clouds = self.api.request("GET", "/api/v1/clouds")
        if not self.cloud:
            if len(clouds) != 1:
                raise RuntimeError("Select --cloud when multiple clouds are configured")
            self.cloud = clouds[0]["id"]
        catalog = self.api.request("GET", "/api/v1/clouds/" + quote(self.cloud, safe=""))
        def choice(explicit, field):
            values = catalog[field]
            if explicit:
                if explicit not in {item["id"] for item in values}:
                    raise RuntimeError(f"Unknown {field}: {explicit}")
                return explicit
            if len(values) != 1:
                raise RuntimeError(f"Select an explicit {field} value")
            return values[0]["id"]
        image = self.args.image
        if image not in {item["id"] for item in catalog["images"]}:
            raise RuntimeError("The generated configuration does not expose the selected image")
        spec = {"name": self.name, "hostname": self.name, "image": image,
                "instance_type": choice(self.args.instance_type, "instance_types"),
                "zone": choice(self.args.zone, "zones"),
                "network": choice(self.args.network, "networks"),
                "storage_class": choice(self.args.storage_class, "storage_classes"), "start": False}
        marker = "VMANGO_INSTALL_OK_" + uuid.uuid4().hex
        serial_device = "/dev/ttyAMA0" if "aarch64" in image else "/dev/ttyS0"
        command = (f"exec > {serial_device} 2>&1; set -eu; test \"$(hostname)\" = '{self.name}'; "
                   "test -b /dev/vda; ip -4 -o addr show scope global | grep -q ' inet '; "
                   "gateway=$(ip -4 route show default | awk '{print $3; exit}'); test -n \"$gateway\"; "
                   "ping -c 1 -W 5 \"$gateway\"; "
                   f"printf '%s%s\\n' 'VMANGO_INSTALL_OK_' '{marker.removeprefix('VMANGO_INSTALL_OK_')}'")
        # Split marker arguments avoid mistaking echoed user-data for successful execution.
        spec["user_data"] = "#cloud-config\nruncmd:\n  - " + json.dumps(["sh", "-c", command]) + "\n"
        self.summary.update({"cloud": self.cloud, "image": image, "zone": spec["zone"],
                             "network": spec["network"], "backend_network": self.args.backend_network})
        self.check("Authenticated against the server using the installer-generated configuration")
        self.task("POST", "/api/v1/clouds/" + quote(self.cloud, safe="") + "/machines", spec)
        if not self.vm_id:
            raise RuntimeError("Create task did not return a machine identity")
        machine = self.api.request("GET", self.machine_path())
        if machine["name"] != self.name or not machine["managed"]:
            raise RuntimeError("Created machine identity or management ownership differs")
        self.record("created-machine.json", machine)
        self.summary["host"] = machine["node"]
        self.disks = [item["volume_id"] for item in machine["disks"]]
        if not any(item["device"] == "disk" and item.get("alias") != "configdrive" for item in machine["disks"]):
            raise RuntimeError("VM has no boot disk")
        if not any(item.get("alias") == "configdrive" for item in machine["disks"]):
            raise RuntimeError("VM has no config drive")
        if not any(item["network"] == self.args.backend_network for item in machine["interfaces"]):
            raise RuntimeError("VM did not use the selected installed network")
        self.check("Created VM, boot disk and config drive on the installed network and storage")
        self.api.login()
        self.task("POST", self.machine_path() + "/actions", {"action": "start"})
        machine = self.api.request("GET", self.machine_path())
        if machine["state"] != "running":
            raise RuntimeError("VM did not enter running state")
        self.check("VM started through Vmango")
        serial = WebSocket(self.api, self.machine_path() + "/consoles/serial/ws", self.args.boot_timeout)
        try:
            vnc = WebSocket(self.api, self.machine_path() + "/consoles/vnc/ws", 15)
            try:
                greeting = b""
                while len(greeting) < 12:
                    greeting += vnc.receive()
                if not greeting.startswith(b"RFB "):
                    raise RuntimeError("VNC proxy did not return an RFB greeting")
            finally:
                vnc.close()
            self.check("VNC WebSocket proxy returned an RFB greeting")
            tail = b""
            deadline = time.monotonic() + self.args.boot_timeout
            with (self.root / "serial.log").open("wb") as output:
                os.chmod(self.root / "serial.log", 0o600)
                while marker.encode() not in tail:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise RuntimeError("Timed out waiting for the cloud-init verification marker")
                    serial.socket.settimeout(remaining)
                    data = serial.receive()
                    output.write(data)
                    output.flush()
                    tail = (tail + data)[-65536:]
        finally:
            serial.close()
        self.check("Serial console confirmed cloud-init, hostname, root disk, IPv4, default route and gateway reachability")
        self.record("running-machine.json", self.api.request("GET", self.machine_path()))

    def cleanup(self):
        if self.create_task:
            task = self.api.request("GET", self.create_task)
            if task["state"] not in TERMINAL:
                raise RuntimeError("Create task is still active; refusing to race it with cleanup")
        if not self.vm_id:
            self.summary["cleanup"] = "No VM identity was allocated"
            return
        machine = self.api.request("GET", self.machine_path())
        if machine["id"] != self.vm_id or machine["name"] != self.name or not machine["managed"]:
            raise RuntimeError("Refusing cleanup: VM identity or ownership changed")
        if machine["state"] != "stopped":
            self.task("POST", self.machine_path() + "/actions", {"action": "poweroff"})
        self.task("DELETE", self.machine_path() + "?delete_volumes=true")
        self.api.request("GET", self.machine_path(), status=404)
        for disk in self.disks:
            self.api.request("GET", "/api/v1/clouds/" + quote(self.cloud, safe="") + "/volumes/" + quote(disk, safe=""), status=404)
        self.summary["cleanup"] = "Deleted only this test VM and its attached disks; host networks, pools and image cache preserved"
        self.check("Test VM and attached disks were deleted")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Exact installer-generated config used by the running server (read only)")
    parser.add_argument("--url", required=True, help="Running server origin, for example http://localhost:8080")
    parser.add_argument("--namespace", default="platform")
    parser.add_argument("--cloud")
    parser.add_argument("--zone")
    parser.add_argument("--image", default="ubuntu-24.04-x86_64")
    parser.add_argument("--instance-type", default="small")
    parser.add_argument("--network", default="lan")
    parser.add_argument("--backend-network", required=True, help="Expected libvirt network name on the selected test hosts (the catalog network ID can differ)")
    parser.add_argument("--storage-class", default="standard")
    parser.add_argument("--task-timeout", type=int, default=1200)
    parser.add_argument("--boot-timeout", type=int, default=1200)
    parser.add_argument("--output", help="New private evidence directory; must not already exist")
    args = parser.parse_args()
    origin = urlsplit(args.url)
    if origin.scheme not in ("http", "https") or not origin.hostname or origin.path not in ("", "/") or origin.username or origin.query or origin.fragment:
        parser.error("--url must be an HTTP(S) origin without credentials, path, query or fragment")
    if not os.environ.get("VMANGO_TEST_PASSWORD"):
        parser.error("set VMANGO_TEST_PASSWORD in the environment")
    if args.task_timeout < 1 or args.boot_timeout < 1:
        parser.error("timeouts must be positive")
    test = Lifecycle(args)
    failure = None
    try:
        test.run()
    except (Exception, KeyboardInterrupt) as error:
        failure = str(error) or type(error).__name__
        print("FAIL:", failure, file=sys.stderr, flush=True)
    finally:
        try:
            test.cleanup()
        except Exception as error:
            test.summary["cleanup"] = "FAILED: " + str(error)
            failure = failure or "Cleanup failed"
            print("CLEANUP FAILED:", error, file=sys.stderr, flush=True)
        current = hashlib.sha256(Path(args.config).read_bytes()).hexdigest()
        test.summary["config_unchanged"] = current == test.summary["config_sha256"]
        if not test.summary["config_unchanged"]:
            failure = failure or "Configuration changed during acceptance"
        test.summary["success"] = failure is None
        if failure:
            test.summary["error"] = failure
        test.record("summary.json", test.summary)
        print("Evidence:", test.root, flush=True)
    return 1 if failure else 0


if __name__ == "__main__":
    sys.exit(main())
