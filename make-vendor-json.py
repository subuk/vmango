#!/usr/bin/env python

import os
import json
import subprocess as sp


def fetch_git_commit(dir):
    cwd = os.getcwd()
    os.chdir(dir)
    commithash = sp.check_output(["git", "rev-parse", "HEAD"])
    os.chdir(cwd)
    return commithash.strip()


def main():
    all_packages = {}
    if os.path.isfile('vendor.json'):
        with open('vendor.json') as f:
            vendorspec = json.load(f)
            for package in vendorspec['package']:
                all_packages[package['path']] = package

    for root, dirs, files in os.walk("vendor/", topdown=False):
        for name in dirs:
            if name != '.git':
                continue
            package_path = root[11:]  # strip vendor/src/ prefix
            all_packages[package_path] = {
                "path": package_path,
                "revision": fetch_git_commit(root),
            }

    vendorspec = {"package": []}
    for package in all_packages.values():
        vendorspec["package"].append(package)

    with open('vendor.json', 'wb') as f:
        json.dump(vendorspec, f, indent=4)

if __name__ == '__main__':
    main()
