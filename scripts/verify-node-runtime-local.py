#!/usr/bin/env python3
"""Collect exact-image Node/TLS/crypto/SQLite/backport proof without external egress."""

import argparse
import json
import os
import secrets
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--reports", type=Path, required=True)
parser.add_argument("--pds-image", default="zing-pds:release-check")
parser.add_argument("--plc-image", default="zing-plc:release-check")
args = parser.parse_args()
os.umask(0o077)
args.reports.mkdir(parents=True, exist_ok=True)


def run(command):
    process = subprocess.run(command, capture_output=True, text=True, timeout=180)
    if process.returncode:
        (args.reports / "runtime-failure.log").write_text(process.stderr)
        raise SystemExit(
            "Runtime validation failed; inspect private runtime-failure.log"
        )
    return process.stdout


with tempfile.TemporaryDirectory(prefix="zing-runtime-tls-") as temporary:
    folder = Path(temporary)
    cert, key = folder / "cert.pem", folder / "key.pem"
    run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "1",
            "-subj",
            "/CN=zing-local-fixture",
            "-addext",
            "subjectAltName=IP:127.0.0.1",
            "-keyout",
            str(key),
            "-out",
            str(cert),
        ]
    )
    # The private parent stays 0700; individual read-only bind mounts let the
    # container's non-root UID read only this disposable certificate/key pair.
    cert.chmod(0o444)
    key.chmod(0o444)
    for name, image in [("pds", args.pds_image), ("plc", args.plc_image)]:
        image_id = run(
            ["docker", "image", "inspect", "--format", "{{.Id}}", image]
        ).strip()
        container = "zing-runtime-" + secrets.token_hex(6)
        command = [
            "docker",
            "run",
            "--rm",
            "--name",
            container,
            "--network",
            "none",
            "--read-only",
            "--user",
            "65532:65532",
            "--cap-drop",
            "ALL",
            "--security-opt",
            "no-new-privileges:true",
            "--tmpfs",
            "/tmp:rw,noexec,nosuid,size=16m",
            "-v",
            str(cert) + ":/fixture/cert.pem:ro",
            "-v",
            str(key) + ":/fixture/key.pem:ro",
            image_id,
            "/services/security/node-runtime-check.mjs",
            "--tls",
        ]
        if name == "pds":
            command.append("--pds")
        try:
            runtime = json.loads(run(command))
        finally:
            subprocess.run(
                ["docker", "rm", "-f", container], capture_output=True, timeout=30
            )
        if runtime["status"] != "ok":
            raise SystemExit("Runtime did not report successful validation")
        tampered = folder / (name + "-tampered.json")
        invalid = {**runtime["nodeBackports"], "nodeSha256": "0" * 64}
        tampered.write_text(json.dumps(invalid))
        tampered.chmod(0o444)
        negative = command.copy()
        position = negative.index(image_id)
        negative[position:position] = [
            "-v",
            str(tampered) + ":/usr/share/zing/node-backports.json:ro",
        ]
        try:
            rejected = subprocess.run(
                negative, capture_output=True, text=True, timeout=180
            )
        finally:
            subprocess.run(
                ["docker", "rm", "-f", container], capture_output=True, timeout=30
            )
        if not (
            rejected.returncode
            and "ERR_ASSERTION" in rejected.stderr
            and "0" * 64 in rejected.stderr
        ):
            raise SystemExit(
                "Tampered runtime manifest did not trigger the expected hash rejection"
            )
        facts = {
            "imageId": image_id,
            "runtime": runtime,
            "tamperedBuildProofRejected": True,
        }
        if name == "pds":
            facts["posixAlignedAllocationOverflowRejected"] = runtime["nodeBackports"][
                "posixAlignedAllocationOverflowRejected"
            ]
        (args.reports / (name + "-runtime.json")).write_text(
            json.dumps(facts, indent=2) + "\n"
        )
        print(json.dumps({"image": name, "imageId": image_id, "status": "ok"}))
