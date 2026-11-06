#!/usr/bin/env python3
"""Rebuild stable GoToSocial with Zing's reviewed dependency graph; no user data."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
URL = "https://codeberg.org/superseriousbusiness/gotosocial/archive/v0.22.3.tar.gz"
SHA256 = "c7096b06f6a70136414926a659da57b4b3c75edcbd79d052d2880898e473e6bb"
VERSION = "0.22.3-zing-security.1"


def build(output):
    version = subprocess.check_output(["go", "env", "GOVERSION"], text=True).strip()
    match = re.fullmatch(r"go(\d+)\.(\d+)\.(\d+)", version)
    if not match or tuple(map(int, match.groups())) < (1, 27, 3):
        raise RuntimeError("Use Go 1.27.3 or a newer supported patched toolchain")
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="zing-activitypub-build-") as folder:
        folder = Path(folder)
        archive = folder / "upstream.tar.gz"
        digest = hashlib.sha256()
        with urllib.request.urlopen(URL, timeout=90) as response, archive.open(
            "wb"
        ) as stream:
            while block := response.read(1024 * 1024):
                digest.update(block)
                stream.write(block)
        if digest.hexdigest() != SHA256:
            raise RuntimeError("Upstream source checksum differs")
        with tarfile.open(archive, "r:gz") as tar:
            tar.extractall(folder, filter="data")
        source = folder / "gotosocial"
        for name in ["go.mod", "go.sum"]:
            shutil.copyfile(
                ROOT / "services/activitypub/security" / name, source / name
            )
        candidate = folder / "gotosocial-security"
        subprocess.run(
            [
                "go",
                "build",
                "-mod=readonly",
                "-p",
                "2",
                "-trimpath",
                "-tags",
                "netgo osusergo static_build timetzdata nomsgpack stdlibjson",
                "-ldflags",
                "-s -w -X main.Version=" + VERSION,
                "-o",
                str(candidate),
                "./cmd/gotosocial",
            ],
            cwd=source,
            env=dict(os.environ, CGO_ENABLED="0", GOMAXPROCS="2"),
            check=True,
        )
        # Keep any existing executable intact until the replacement is complete.
        temporary = output.with_name(output.name + ".security-new")
        shutil.copyfile(candidate, temporary)
        temporary.chmod(0o755)
        temporary.replace(output)
    print("Built " + VERSION + "; restart the native supervisor to use it.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--output", type=Path, default=ROOT / ".lidza/runtime/gotosocial/gotosocial"
    )
    args = parser.parse_args()
    build(args.output)
