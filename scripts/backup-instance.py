#!/usr/bin/env python3
"""Encrypted, quiesced file/PG snapshots and safe restore into a NEW directory.

The private JSON config has databases {alias: DSN}, paths {alias: path}.
Stop all application/native writers first; use storage snapshots for remote S3.
The age identity is independent of the application master key and never bundled.
"""
import argparse, hmac, hashlib, json, os, pathlib, re, shutil, subprocess, tarfile, tempfile, urllib.parse


def run(args, **kw):
    result = subprocess.run(args, capture_output=True, **kw)
    if result.returncode:
        raise RuntimeError(
            pathlib.Path(args[0]).name + " failed; no restore was applied"
        )
    return result.stdout


def pg_env(dsn):
    u = urllib.parse.urlsplit(dsn)
    if u.scheme not in ("postgres", "postgresql"):
        raise ValueError("Use a PostgreSQL URI")
    allowed = {
        "host": "PGHOST",
        "port": "PGPORT",
        "user": "PGUSER",
        "password": "PGPASSWORD",
        "dbname": "PGDATABASE",
        "sslmode": "PGSSLMODE",
        "sslcert": "PGSSLCERT",
        "sslkey": "PGSSLKEY",
        "sslrootcert": "PGSSLROOTCERT",
        "connect_timeout": "PGCONNECT_TIMEOUT",
        "application_name": "PGAPPNAME",
        "options": "PGOPTIONS",
        "target_session_attrs": "PGTARGETSESSIONATTRS",
        "channel_binding": "PGCHANNELBINDING",
    }
    result = {k: v for k, v in os.environ.items() if not k.startswith("PG")}
    result["PGDATABASE"] = urllib.parse.unquote(u.path.lstrip("/")) or "postgres"
    if u.hostname:
        result["PGHOST"] = u.hostname
    if u.port:
        result["PGPORT"] = str(u.port)
    if u.username:
        result["PGUSER"] = urllib.parse.unquote(u.username)
    if u.password:
        result["PGPASSWORD"] = urllib.parse.unquote(u.password)
    for key, value in urllib.parse.parse_qsl(u.query):
        if key not in allowed:
            raise ValueError("Unsupported PostgreSQL URI option")
        result[allowed[key]] = value
    return result


def pg_tool(tool, dsn):
    env = pg_env(dsn)
    major = (
        int(run(["psql", "-Atc", "SHOW server_version_num"], env=env).strip()) // 10000
    )
    candidate = pathlib.Path("/usr/lib/postgresql") / str(major) / "bin" / tool
    program = str(candidate) if candidate.exists() else shutil.which(tool)
    if not program:
        raise ValueError("Install matching PostgreSQL client tools")
    version = run([program, "--version"]).decode()
    match = re.search(r"PostgreSQL\) (\d+)", version)
    if not match or int(match[1]) != major:
        raise ValueError(
            "Use PostgreSQL dump/restore tools matching the server major version"
        )
    return program


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def verify(root):
    manifest = json.loads((root / "manifest.json").read_text())
    if manifest.get("format") != 1:
        raise ValueError("Unsupported snapshot format")
    actual = {
        str(p.relative_to(root))
        for p in root.rglob("*")
        if p.is_file() and p != root / "manifest.json"
    }
    if actual != set(manifest["files"]):
        raise ValueError("Snapshot file set differs from its manifest")
    for name, expected in manifest["files"].items():
        p = pathlib.PurePosixPath(name)
        if p.is_absolute() or ".." in p.parts:
            raise ValueError("Unsafe snapshot path")
        if digest(root / name) != expected:
            raise ValueError("Snapshot content hash differs")
    return manifest


def backup(config, out, recipient, quiesced):
    if not quiesced:
        raise ValueError("Stop writers and explicitly pass --quiesced")
    if out.exists():
        raise ValueError("Refusing to replace an existing backup")
    out.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="zing-backup-", dir=out.parent) as temp:
        root = pathlib.Path(temp) / "snapshot"
        root.mkdir(mode=0o700)
        counts = {}
        for alias, dsn in config["databases"].items():
            if not re.fullmatch(r"[a-z][a-z0-9_-]{0,30}", alias):
                raise ValueError("Invalid database alias")
            folder = root / "databases"
            folder.mkdir(exist_ok=True)
            env = pg_env(dsn)
            run(
                [
                    pg_tool("pg_dump", dsn),
                    "--format=custom",
                    "--no-owner",
                    "--no-acl",
                    "--file",
                    str(folder / (alias + ".dump")),
                ],
                env=env,
            )
            names = (
                run(
                    [
                        "psql",
                        "-Atc",
                        "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename",
                    ],
                    env=env,
                )
                .decode()
                .splitlines()
            )
            counts[alias] = {}
            for name in names:
                quoted = '"' + name.replace('"', '""') + '"'
                counts[alias][name] = int(
                    run(["psql", "-Atc", "SELECT count(*) FROM " + quoted], env=env)
                    .decode()
                    .strip()
                )
        for alias, source in config["paths"].items():
            if not re.fullmatch(r"[a-z][a-z0-9_-]{0,30}", alias):
                raise ValueError("Invalid path alias")
            source = pathlib.Path(source).resolve(strict=True)
            if source.is_dir() and any(p.is_symlink() for p in source.rglob("*")):
                raise ValueError("Snapshot paths must not contain symbolic links")
            dest = root / "paths" / alias
            dest.parent.mkdir(exist_ok=True)
            if source.is_dir():
                shutil.copytree(source, dest)
            else:
                shutil.copy2(source, dest)
        manifest = {
            "format": 1,
            "databases": list(config["databases"]),
            "counts": counts,
            "files": {
                str(p.relative_to(root)): digest(p)
                for p in root.rglob("*")
                if p.is_file()
            },
        }
        (root / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        archive = pathlib.Path(temp) / "snapshot.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            for p in sorted(root.rglob("*")):
                tar.add(p, arcname=p.relative_to(root), recursive=False)
        cipher = pathlib.Path(temp) / "snapshot.age"
        run(
            [
                "age",
                "--encrypt",
                "--recipient",
                recipient,
                "--output",
                str(cipher),
                str(archive),
            ]
        )
        os.chmod(cipher, 0o600)
        # Both paths share the output filesystem. Publish atomically without
        # replacing a snapshot created by another backup process meanwhile.
        os.link(cipher, out)

    return {
        "encrypted": True,
        "databaseCount": len(counts),
        "fileCount": len(manifest["files"]),
        "sha256": digest(out),
    }


def restore(archive, identity, destination, expected_sha256):
    if not re.fullmatch(r"[0-9a-f]{64}", expected_sha256) or not hmac.compare_digest(
        digest(archive), expected_sha256
    ):
        raise ValueError("Backup differs from the trusted backup inventory hash")
    if destination.exists():
        raise ValueError("Restore destination must not already exist")
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(
        prefix="zing-restore-", dir=destination.parent
    ) as temp:
        decrypted = pathlib.Path(temp) / "snapshot.tar.gz"
        run(
            [
                "age",
                "--decrypt",
                "--identity",
                str(identity),
                "--output",
                str(decrypted),
                str(archive),
            ]
        )
        staging = pathlib.Path(temp) / "checked"
        staging.mkdir(mode=0o700)
        with tarfile.open(decrypted, "r:gz") as tar:
            members = tar.getmembers()
            seen = set()
            for m in members:
                p = pathlib.PurePosixPath(m.name)
                if (
                    p.is_absolute()
                    or ".." in p.parts
                    or m.name in seen
                    or not (m.isfile() or m.isdir())
                ):
                    raise ValueError("Unsafe snapshot member")
                seen.add(m.name)
            tar.extractall(staging, members=members, filter="data")
        manifest = verify(staging)
        staging.rename(destination)
    return {
        "verified": True,
        "databaseCount": len(manifest["databases"]),
        "fileCount": len(manifest["files"]),
    }


def main():
    os.umask(0o077)
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="action", required=True)
    b = sub.add_parser("backup")
    b.add_argument("--config", type=pathlib.Path, required=True)
    b.add_argument("--out", type=pathlib.Path, required=True)
    b.add_argument("--recipient", required=True)
    b.add_argument("--quiesced", action="store_true")
    r = sub.add_parser("restore")
    r.add_argument("--archive", type=pathlib.Path, required=True)
    r.add_argument("--identity", type=pathlib.Path, required=True)
    r.add_argument(
        "--expected-sha256",
        required=True,
        help="Hash recorded by the trusted backup inventory",
    )
    r.add_argument("--destination", type=pathlib.Path, required=True)
    a = p.parse_args()
    if a.action == "backup":
        result = backup(
            json.loads(a.config.read_text()), a.out, a.recipient, a.quiesced
        )
    else:
        result = restore(a.archive, a.identity, a.destination, a.expected_sha256)
    print(json.dumps(result))


if __name__ == "__main__":
    main()
