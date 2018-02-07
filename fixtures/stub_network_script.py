#!/usr/bin/env python
from __future__ import print_function
import os
import sys


def print_context():
    print("===",sys.argv[1:])
    for name, value in os.environ.items():
        if not name.startswith("VMANGO_"):
            continue
        print("%s: %s" % (name, value))


def lookup():
    print("44.43.42.41")


def assign():
    print("44.43.42.41")


def release():
    pass


def main():
    handlers = {
        "lookup-ip": lookup,
        "assign-ip": assign,
        "release-ip": release,
    }
    action = sys.argv[1]
    handler = handlers.get(action)
    if handler is None:
        print("Unknown action requested: %s" % action)
        return 1
    return handler()


if __name__ == '__main__':
    sys.exit(main() or 0)