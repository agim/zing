#!/usr/bin/env python3
"""Actual encrypted backup/restore drill. Original databases/files are never restored over."""
import importlib.util, json, os, pathlib, sqlite3, subprocess, tempfile, time, urllib.request, http.cookiejar, uuid

ROOT = pathlib.Path(__file__).resolve().parent.parent
os.chdir(ROOT)
os.umask(0o077)
spec = importlib.util.spec_from_file_location(
    "snapshot", ROOT / "scripts/backup-instance.py"
)
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)


def command(args, **kwargs):
    return backup.run(args, **kwargs)


def dsn(name):
    return "postgres:///" + name + "?host=/var/run/postgresql"


def counts(database):
    env = backup.pg_env(database)
    tables = (
        command(
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
    return {
        name: int(
            command(
                [
                    "psql",
                    "-Atc",
                    'SELECT count(*) FROM "' + name.replace('"', '""') + '"',
                ],
                env=env,
            )
        )
        for name in tables
    }


def ready(url, process):
    for _ in range(150):
        if process.poll() is not None:
            raise RuntimeError(
                "Isolated recovery service stopped; private logs retained"
            )
        try:
            with urllib.request.urlopen(url, timeout=1) as r:
                if r.status == 200:
                    return
        except OSError:
            pass
        time.sleep(0.2)
    raise RuntimeError("Isolated recovery readiness timed out")


# The local reference profile must be quiescent; do not stop arbitrary user processes.
for proc in pathlib.Path("/proc").glob("[0-9]*"):
    try:
        if pathlib.Path(os.readlink(proc / "cwd")) != ROOT:
            continue
        args = (proc / "cmdline").read_bytes().split(b"\0")
        if any(
            a in [b"./scripts/local.sh", b"scripts/social-local.py", b"./bin/zing"]
            for a in args
        ):
            raise RuntimeError("Stop the local launchers before the recovery drill")
    except (PermissionError, FileNotFoundError, ProcessLookupError):
        pass

folder = ROOT / ".lidza/recovery"
folder.mkdir(exist_ok=True, mode=0o700)
identity = folder / "recovery.agekey"
if not identity.exists():
    command(["age-keygen", "--output", str(identity)])
os.chmod(identity, 0o600)
recipient = command(["age-keygen", "-y", str(identity)]).decode().strip()
archive = folder / ("snapshot-" + uuid.uuid4().hex[:12] + ".tar.gz.age")
paths = {
    "config": str(ROOT / "config"),
    "pds": str(ROOT / ".lidza/social/pds"),
    "activitypub": str(ROOT / ".lidza/social/gotosocial"),
    "agent": str(ROOT / ".lidza/social/agent"),
    "social-secrets": str(ROOT / ".lidza/social/secrets.json"),
    "activitypub-config": str(ROOT / ".lidza/social/gotosocial.json"),
}
for alias, path in [("storage", ROOT / "storage"), ("environment", ROOT / ".env")]:
    if path.exists():
        paths[alias] = str(path)
config = {"databases": {"app": dsn("zing_dev"), "plc": dsn("zing_plc")}, "paths": paths}
created = []
children = []
logs = []
try:
    backup.backup(config, archive, recipient, True)
    with tempfile.TemporaryDirectory(prefix="drill-", dir=folder) as temp:
        temp = pathlib.Path(temp)
        restored = temp / "restored"
        backup.restore(archive, identity, restored, backup.digest(archive))
        manifest = backup.verify(restored)
        corrupted = temp / "corrupt.age"
        bad = bytearray(archive.read_bytes())
        bad[-1] ^= 1
        corrupted.write_bytes(bad)
        try:
            backup.restore(
                corrupted, identity, temp / "must-not-exist", backup.digest(corrupted)
            )
        except RuntimeError:
            pass
        else:
            raise AssertionError("Tampered encrypted backup accepted")
        assert not (temp / "must-not-exist").exists()
        for alias in manifest["databases"]:
            name = "zing_restore_" + alias + "_" + uuid.uuid4().hex[:10]
            command(["createdb", name])
            created.append(name)
            command(
                [
                    backup.pg_tool("pg_restore", dsn(name)),
                    "--dbname",
                    name,
                    "--no-owner",
                    "--no-acl",
                    "--exit-on-error",
                    str(restored / "databases" / (alias + ".dump")),
                ],
                env=backup.pg_env(dsn(name)),
            )
            assert (
                counts(dsn(name)) == manifest["counts"][alias]
            ), "Restored table counts differ"
        sqlite_count = 0
        for file in (restored / "paths").rglob("*"):
            if not file.is_file():
                continue
            with file.open("rb") as f:
                header = f.read(16)
            if header != b"SQLite format 3\0":
                continue
            connection = sqlite3.connect("file:" + str(file) + "?mode=ro", uri=True)
            try:
                connection.execute("PRAGMA trusted_schema=OFF")
                assert connection.execute("PRAGMA integrity_check").fetchall() == [
                    ("ok",)
                ]
            finally:
                connection.close()
            sqlite_count += 1
        assert sqlite_count >= 3, "Native SQLite stores were not included"
        runtime = restored / "paths"
        env = dict(
            os.environ,
            DATABASE_URL=dsn(created[0]),
            LIDZA_MODE="dev",
            LIDZA_MASTER_KEY="",
        )
        proof = json.loads(
            command([str(ROOT / "bin/recovery-check")], env=env, cwd=runtime)
        )
        wrong = subprocess.run(
            [str(ROOT / "bin/recovery-check")],
            env=dict(env, LIDZA_MASTER_KEY="00" * 32),
            cwd=runtime,
            capture_output=True,
        )
        assert wrong.returncode != 0, "Wrong application key accepted"

        def start(name, args, extra, cwd=ROOT):
            log = (temp / (name + ".log")).open("wb")
            logs.append(log)
            process = subprocess.Popen(
                args, cwd=cwd, env=dict(os.environ, **extra), stdout=log, stderr=log
            )
            children.append(process)
            return process

        plc = start(
            "plc",
            ["node", "services/dev-plc/start.mjs"],
            {"ZING_PLC_DATABASE_URL": dsn(created[1]), "ZING_PLC_PORT": "12582"},
        )
        ready("http://127.0.0.1:12582/_health", plc)
        secrets = json.loads((runtime / "social-secrets").read_text())
        pds = start(
            "pds",
            ["node", "services/pds/start.mjs"],
            {
                "PDS_HOSTNAME": "localhost",
                "PDS_PORT": "12583",
                "PDS_DATA_DIRECTORY": str(runtime / "pds"),
                "PDS_BLOBSTORE_DISK_LOCATION": str(runtime / "pds/blobs"),
                "PDS_DID_PLC_URL": "http://127.0.0.1:12582",
                "PDS_JWT_SECRET": secrets["pdsJwt"],
                "PDS_ADMIN_PASSWORD": secrets["pdsAdmin"],
                "PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX": secrets["pdsRotation"],
                "PDS_DEV_MODE": "true",
                "PDS_INVITE_REQUIRED": "true",
                "LOG_ENABLED": "false",
                "ZING_BIND_LOOPBACK": "true",
            },
        )
        ready("http://127.0.0.1:12583/xrpc/_health", pds)
        proof = json.loads(
            command(
                [str(ROOT / "bin/recovery-check"), "--pds", "http://127.0.0.1:12583"],
                env=env,
                cwd=runtime,
            )
        )
        app = start(
            "app",
            [str(ROOT / "bin/zing")],
            dict(
                env,
                LIDZA_ADDR="127.0.0.1:13001",
                APP_URL="http://127.0.0.1:13001",
                AUTH_SECRET="recovery-drill-only-session-secret-0123456789",
                AUTH_COOKIE_SECURE="false",
                AUTH_OWNER_CLAIM="false",
                JOBS_WORKERS="0",
                DB_MIGRATE="false",
                CACHE_URL="memory",
                REALTIME_BUS_URL="",
                MAIL_PROVIDER="outbox",
                STORAGE_PROVIDER="local",
                STORAGE_DIR=str(runtime / "storage"),
            ),
            cwd=runtime,
        )
        ready("http://127.0.0.1:13001/readyz", app)
        access = json.loads((ROOT / ".lidza/local-access.json").read_text())
        opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
        )
        request = urllib.request.Request(
            "http://127.0.0.1:13001/api/v1/auth/login",
            data=json.dumps(
                {"email": access["email"], "password": access["password"]}
            ).encode(),
            headers={"Content-Type": "application/json"},
        )
        with opener.open(request, timeout=10) as r:
            assert r.status == 200
        with opener.open("http://127.0.0.1:13001/api/v1/connections", timeout=10) as r:
            assert len(json.load(r)["items"]) >= 2
        running = int(
            command(
                ["psql", "-Atc", "SELECT count(*) FROM job WHERE state='running'"],
                env=backup.pg_env(dsn(created[0])),
            )
        )
        assert running == 0, "Recovery worker started"
        report = {
            "status": "ok",
            "encryptedBackup": True,
            "freshDatabaseRestore": True,
            "countsMatch": True,
            "allFileHashesMatch": True,
            "nativeSQLiteStoresChecked": sqlite_count,
            "tamperRejectedBeforeExtraction": True,
            "wrongMasterKeyRejected": True,
            "applicationReadyAndLoginVerified": True,
            "outboundWorkersDisabled": True,
            **proof,
        }
        (folder / "validation.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report))
finally:
    for child in reversed(children):
        if child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()
    for log in logs:
        log.close()
    for name in created:
        subprocess.run(
            ["dropdb", "--force", name],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
