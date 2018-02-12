#!/usr/bin/env python
from __future__ import print_function
import os
import sys


def lookup():
    assert "VMANGO_MACHINE_HWADDR" in os.environ
    assert "VMANGO_MACHINE_NAME" in os.environ
    assert "VMANGO_MACHINE_PLAN" in os.environ
    assert "VMANGO_MACHINE_ID" in os.environ
    print("44.43.42.41")


def assign():
    assert "VMANGO_MACHINE_HWADDR" in os.environ
    assert "VMANGO_MACHINE_NAME" in os.environ
    assert "VMANGO_MACHINE_PLAN" in os.environ
    assert "VMANGO_MACHINE_ID" in os.environ
    print("44.43.42.41")


def release():
    assert "VMANGO_MACHINE_HWADDR" in os.environ
    assert "VMANGO_MACHINE_IP" in os.environ
    assert "VMANGO_MACHINE_NAME" in os.environ
    assert "VMANGO_MACHINE_PLAN" in os.environ
    assert "VMANGO_MACHINE_ID" in os.environ


def error():
    sys.stderr.write("Unknown action requested\n")
    return 1

def main():
    handlers = {
        "lookup-ip": lookup,
        "assign-ip": assign,
        "release-ip": release,
    }
    return handlers.get(sys.argv[1], error)()


if __name__ == '__main__':
    sys.exit(main() or 0)