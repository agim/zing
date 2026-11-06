#!/usr/bin/env python3
"""Real age/restore rejection checks; all payloads and keys are disposable fixtures."""
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile

os.umask(0o077)
spec = importlib.util.spec_from_file_location(
    "snapshot", Path(__file__).with_name("backup-instance.py")
)
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


def rejected(action):
    try:
        action()
    except (ValueError, RuntimeError, tarfile.TarError):
        return
    raise AssertionError("Unsafe snapshot was accepted")


with tempfile.TemporaryDirectory(prefix="zing-backup-security-") as folder:
    root = Path(folder)
    identity = root / "identity"
    b.run(["age-keygen", "-o", str(identity)])
    recipient = b.run(["age-keygen", "-y", str(identity)]).decode().strip()
    source = root / "fixture"
    source.write_text("private fixture")
    archive = root / "valid.age"
    config = {"databases": {}, "paths": {"fixture": str(source)}}
    rejected(lambda: b.backup(config, archive, recipient, False))
    inventory = b.backup(config, archive, recipient, True)
    assert inventory["sha256"] == b.digest(archive)
    rejected(lambda: b.backup(config, archive, recipient, True))
    original_run = b.run
    competing = root / "competing.age"

    def concurrent_publish(args, **kwargs):
        result = original_run(args, **kwargs)
        if args[0] == "age" and "--encrypt" in args:
            competing.write_bytes(b"other backup must survive")
        return result

    b.run = concurrent_publish
    try:
        try:
            b.backup(config, competing, recipient, True)
        except FileExistsError:
            pass
        else:
            raise AssertionError("Concurrent backup overwritten")
        assert competing.read_bytes() == b"other backup must survive"
    finally:
        b.run = original_run
    destination = root / "restored"
    b.restore(archive, identity, destination, inventory["sha256"])
    assert (destination / "paths/fixture").read_text() == "private fixture"
    rejected(lambda: b.restore(archive, identity, destination, inventory["sha256"]))
    rejected(
        lambda: b.restore(archive, identity, root / "inventory-rejected", "0" * 64)
    )
    assert not (root / "inventory-rejected").exists()
    corrupt = root / "corrupt.age"
    data = bytearray(archive.read_bytes())
    data[-1] ^= 1
    corrupt.write_bytes(data)
    rejected(
        lambda: b.restore(
            corrupt, identity, root / "corruption-rejected", b.digest(corrupt)
        )
    )
    assert not (root / "corruption-rejected").exists()
    for name, kind, link in [
        ("traversal", tarfile.REGTYPE, ""),
        ("symlink", tarfile.SYMTYPE, "../escape"),
        ("hardlink", tarfile.LNKTYPE, "../escape"),
        ("bad-hash", tarfile.REGTYPE, ""),
    ]:
        plain = root / (name + ".tar.gz")
        with tarfile.open(plain, "w:gz") as tar:
            info = tarfile.TarInfo("../escape" if name == "traversal" else "payload")
            info.type = kind
            info.linkname = link
            if kind == tarfile.REGTYPE:
                info.size = 3
                tar.addfile(info, io.BytesIO(b"bad"))
            else:
                tar.addfile(info)
            manifest = json.dumps(
                {"format": 1, "databases": [], "files": {"payload": "0" * 64}}
            ).encode()
            info = tarfile.TarInfo("manifest.json")
            info.size = len(manifest)
            tar.addfile(info, io.BytesIO(manifest))
        encrypted = root / (name + ".age")
        b.run(["age", "-r", recipient, "-o", str(encrypted), str(plain)])
        target = root / (name + "-rejected")
        rejected(lambda: b.restore(encrypted, identity, target, b.digest(encrypted)))
        assert not target.exists() and not (root / "escape").exists()
    values = b.pg_env(
        "postgres://fixture:p%40ss@example.invalid/fixture?sslmode=verify-full"
    )
    assert (
        values["PGPASSWORD"] == "p@ss"
        and values["PGDATABASE"] == "fixture"
        and values["PGSSLMODE"] == "verify-full"
    )
print(
    json.dumps(
        {
            "status": "ok",
            "roundTrip": True,
            "quiescentBoundaryRequired": True,
            "overwriteRejected": True,
            "concurrentBackupPreserved": True,
            "inventoryMismatchRejected": True,
            "ciphertextTamperRejected": True,
            "pathTraversalRejected": True,
            "symbolicAndHardLinksRejected": True,
            "manifestMismatchRejected": True,
        }
    )
)
