#!/usr/bin/env python3
"""Package the native binary, documentation and (on Windows) runtime DLLs."""
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def package(version, target, binary, runtime=None):
    if not re.fullmatch(r"[0-9A-Za-z][0-9A-Za-z.-]*", version):
        raise ValueError("Unsafe version")
    if target not in ("linux-x64", "macos-arm64", "windows-x64"):
        raise ValueError("Unknown target")
    root = pathlib.Path(__file__).resolve().parents[2]
    dist = root / "dist"
    dist.mkdir(exist_ok=True)
    name = f"vmango-{version}-{target}"
    with tempfile.TemporaryDirectory() as tmp:
        stage = pathlib.Path(tmp) / name
        stage.mkdir()
        shutil.copy2(binary, stage / ("vmango.exe" if target == "windows-x64" else "vmango"))
        for filename in ("LICENSE", "vmango.quickstart.conf", "vmango.dist.conf"):
            shutil.copy2(root / filename, stage)
        shutil.copy2(root / "docs/content/en/docs/start.md", stage / "QUICKSTART.md")
        shutil.copy2(root / "docs/UI-MIGRATION.md", stage / "MIGRATION.md")
        shutil.copy2(root / "docs/API.md", stage / "API.md")
        # Embedded Go and browser dependencies retain their upstream notices.
        notices = stage / "licenses"
        notices.mkdir(exist_ok=True)
        shutil.copy2(root / "ui/dist/THIRD-PARTY-LICENSES.txt", notices / "frontend.txt")
        for directory in (root / "vendor", root / "ui/public/vendor"):
            for path in directory.rglob("*"):
                if path.is_file() and path.name.lower().startswith(("license", "copying", "notice", "copyright")):
                    dest = notices / path.relative_to(root)
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(path, dest)
        if target == "windows-x64":
            if runtime is None:
                raise ValueError("Windows packaging needs the MinGW runtime directory")
            runtime = pathlib.Path(runtime)
            # Include the entire installed runtime closure, including dynamically loaded DLLs.
            dlls = sorted(runtime.glob("*.dll"))
            if not (runtime / "libvirt-0.dll").exists():
                raise ValueError("Missing libvirt runtime")
            packages = set()
            for dll in dlls:
                shutil.copy2(dll, stage)
                packages.add(subprocess.check_output(["rpm", "-qf", "--qf", "%{NAME}", str(dll)], text=True))
            shutil.copy2(runtime / "virsh.exe", stage)
            with (stage / "RUNTIME-PACKAGES.txt").open("w") as manifest:
                manifest.write("Fedora 44 MinGW runtime packages. Sources: https://src.fedoraproject.org/\n\n")
                for pkg in sorted(packages):
                    manifest.write(subprocess.check_output(["rpm", "-q", "--qf", "%{NAME} %{VERSION}-%{RELEASE} source: %{SOURCERPM}\n", pkg], text=True))
                    for filename in subprocess.check_output(["rpm", "-qL", pkg], text=True).splitlines():
                        path = pathlib.Path(filename)
                        if path.is_file():
                            dest = notices / "runtime" / pkg / path.relative_to("/")
                            dest.parent.mkdir(parents=True, exist_ok=True)
                            shutil.copy2(path, dest)
            archive = dist / f"{name}.zip"
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as output:
                for path in sorted(stage.rglob("*")):
                    if path.is_file():
                        output.write(path, path.relative_to(tmp))
        else:
            archive = dist / f"{name}.tar.gz"
            with tarfile.open(archive, "w:gz") as output:
                output.add(stage, arcname=name)
        print(archive)
        return archive


if __name__ == "__main__":
    package(*sys.argv[1:])
