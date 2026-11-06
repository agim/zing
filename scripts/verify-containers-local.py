#!/usr/bin/env python3
"""Disposable Compose acceptance. Uses fresh fixture secrets, no public identities.
Needs Docker + Compose v2.24+, age, and a local free loopback port.
"""
import argparse, traceback, hashlib, http.cookiejar, importlib.util, io, json, os, pathlib, re, secrets, subprocess, tarfile, tempfile, time, urllib.request, uuid

from container_bridge import Bridge

ROOT = pathlib.Path(__file__).resolve().parent.parent
os.chdir(ROOT)
os.umask(0o077)
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--no-build", action="store_true")
parser.add_argument("--port", type=int, default=13000)
parser.add_argument("--proxy")
parser.add_argument(
    "--baseline-pds-image",
    help="Existing glibc image used before the candidate upgrade",
)
parser.add_argument("--baseline-plc-image", help="Matching baseline PLC fixture image")
args = parser.parse_args()
if bool(args.baseline_pds_image) != bool(args.baseline_plc_image) or (
    args.baseline_pds_image and not args.no_build
):
    parser.error("Baseline upgrade checks require both baseline images and --no-build")
run_id = uuid.uuid4().hex[:10]
project = "zing-acceptance-" + run_id
recovery_project = "zing-recovered-" + run_id
work = ROOT / ".lidza/container-acceptance" / run_id
work.mkdir(parents=True, mode=0o700)
values = {
    "AUTH_SECRET": secrets.token_urlsafe(48),
    "LIDZA_MASTER_KEY": secrets.token_hex(32),
    "POSTGRES_PASSWORD": secrets.token_hex(24),
    "ZING_ACTIVITYPUB_DOMAIN": "activitypub:8080",
    "ZING_PDS_DOMAIN": "pds",
    "ZING_ACTIVITYPUB_AGENT_TOKEN": secrets.token_urlsafe(32),
    "PDS_ADMIN_PASSWORD": secrets.token_urlsafe(32),
    "PDS_JWT_SECRET": secrets.token_hex(32),
    "PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX": secrets.token_hex(32),
}
env_file = work / "fixture.env"
env_file.write_text("".join(k + "=" + v + "\n" for k, v in values.items()))
os.chmod(env_file, 0o600)
env = dict(
    os.environ,
    **values,
    ZING_TEST_ENV_FILE=str(env_file),
    ZING_TEST_PORT=str(args.port)
)
if args.proxy:
    env.update(
        HTTP_PROXY=args.proxy,
        HTTPS_PROXY=args.proxy,
        http_proxy=args.proxy,
        https_proxy=args.proxy,
    )
files = [
    "deploy/compose.yaml",
    "deploy/compose.social.yaml",
    "deploy/compose.acceptance.yaml",
]


def docker(*cmd, input=None, log=False):
    result = subprocess.run(["docker", *cmd], env=env, input=input, capture_output=True)
    if log or result.returncode:
        with (work / "docker.log").open("ab") as f:
            f.write(result.stdout)
            f.write(result.stderr)
    if result.returncode:
        raise RuntimeError(
            "Container command failed; inspect private container acceptance log"
        )
    return result.stdout


using_baseline = bool(args.baseline_pds_image)
baseline_override = work / "baseline.json"
if using_baseline:
    baseline_override.write_text(
        json.dumps(
            {
                "services": {
                    "pds": {"image": args.baseline_pds_image},
                    "plc": {"image": args.baseline_plc_image},
                }
            }
        )
    )


def compose(*cmd, clone=False, extra=None, input=None, log=False):
    command = [
        "compose",
        "--project-name",
        recovery_project if clone else project,
        "--env-file",
        str(env_file),
    ]
    for file in files:
        command += ["-f", file]
    if using_baseline and not clone:
        command += ["-f", str(baseline_override)]
    if extra:
        command += ["-f", str(extra)]
    return docker(*command, *cmd, input=input, log=log)


def wait(fn, label, seconds=120):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            result = fn()
            if result:
                return result
        except (OSError, urllib.error.HTTPError):
            pass
        time.sleep(0.4)
    raise RuntimeError("Timed out waiting for " + label)


base = "http://127.0.0.1:" + str(args.port)
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def api(path, body=None, method=None, raw=None):
    data = (
        raw
        if raw is not None
        else json.dumps(body).encode() if body is not None else None
    )
    req = urllib.request.Request(
        base + "/api/v1/" + path,
        data=data,
        method=method,
        headers={
            "Content-Type": "image/png" if raw is not None else "application/json",
            "Sec-Fetch-Site": "same-origin",
            "Origin": base,
        },
    )
    with client.open(req, timeout=30) as response:
        payload = response.read()
        return json.loads(payload) if payload and raw is None else payload


def pg(query, clone=False, database="zing"):
    return (
        compose(
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            "zing",
            "-d",
            database,
            "-Atc",
            query,
            clone=clone,
        )
        .decode()
        .strip()
    )


def snapshot_counts(clone=False):
    counts = {}
    for database in ["zing", "zing_plc"]:
        tables = pg(
            "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename",
            clone,
            database,
        ).splitlines()
        counts[database] = {
            table: int(
                pg(
                    'SELECT count(*) FROM "' + table.replace('"', '""') + '"',
                    clone,
                    database,
                )
            )
            for table in tables
        }
    return counts


def native_blob(url, clone=False):
    js = "fetch(process.argv[1]).then(async r=>{if(!r.ok)process.exit(1);process.stdout.write(Buffer.from(await r.arrayBuffer()))}).catch(()=>process.exit(1))"
    return compose("exec", "-T", "pds", "/nodejs/bin/node", "-e", js, url, clone=clone)


def native_record(post, clone=False):
    parts = post["id"].split("/")
    assert post["id"].startswith("at://") and len(parts) == 5
    url = "http://pds:2583/xrpc/com.atproto.repo.getRecord?" + urllib.parse.urlencode(
        {"repo": parts[2], "collection": parts[3], "rkey": parts[4]}
    )
    try:
        record = json.loads(native_blob(url, clone))
    except RuntimeError:
        return False
    return (
        record.get("uri") == post["id"]
        and record.get("cid") == post["cid"]
        and record.get("value", {}).get("text") == post["text"]
    )


def postgres_ready(clone=False):
    try:
        return bool(
            compose(
                "exec",
                "-T",
                "postgres",
                "sh",
                "-c",
                'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U zing -d zing -Atc "SELECT 1"',
                clone=clone,
            ).strip()
            == b"1"
        )
    except RuntimeError:
        return False


def ready():
    with urllib.request.urlopen(base + "/readyz", timeout=2) as r:
        return r.status == 200


def volume_name(service, destination):
    container = compose("ps", "--all", "-q", service).decode().strip()
    info = json.loads(docker("inspect", container))[0]
    return next(m["Name"] for m in info["Mounts"] if m["Destination"] == destination)


recovering = False


def app_address():
    container = compose("ps", "-q", "app", clone=recovering).decode().strip()
    info = json.loads(docker("inspect", container))[0]
    return (
        next(
            v["IPAddress"]
            for v in info["NetworkSettings"]["Networks"].values()
            if v.get("IPAddress")
        ),
        3000,
    )


bridge = Bridge(args.port, app_address)
cloned_volumes = []
clone_override = work / "recovery.yaml"
report = {}
try:
    if not args.no_build:
        command = ["build"]
        for key in ["HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"]:
            if key in env:
                command += ["--build-arg", key]
        compose(*command, "app", "activitypub", "pds", "plc", "postgres", log=True)
    compose("up", "-d", "postgres", "valkey", "mail", log=True)
    wait(postgres_ready, "PostgreSQL")
    compose("exec", "-T", "postgres", "createdb", "-U", "zing", "zing_plc", log=True)
    compose("up", "-d", log=True)
    wait(ready, "application readiness")
    user = api(
        "auth/register",
        {
            "email": "container-" + run_id + "@zing.invalid",
            "name": "Container signup fixture",
            "password": "Fixture-" + secrets.token_urlsafe(24),
        },
    )
    owner = user["subject"]
    assert (
        pg("SELECT count(*) FROM hosted_identity WHERE owner_id='" + owner + "'") == "0"
    )
    assert (
        pg("SELECT count(*) FROM hosted_enrollment WHERE owner_id='" + owner + "'")
        == "1"
    )

    def token():
        mail = json.loads(
            compose(
                "exec",
                "-T",
                "mail",
                "python3",
                "-c",
                'import urllib.request;print(urllib.request.urlopen("http://127.0.0.1:8025/messages").read().decode())',
            )
        )
        for message in mail:
            if "container-" + run_id + "@zing.invalid" in message["to"]:
                found = re.search(r"[?&]token=([A-Za-z0-9_-]+)", message["text"])
                if found:
                    return found[1]

    verification = wait(token, "real fixture SMTP delivery")
    compose("stop", "pds", log=True)
    api("auth/verify", {"token": verification})

    def partial():
        status = api("hosting")
        if any(
            i["network"] == "mastodon" and i["state"] == "active"
            for i in status["identities"]
        ) and any(
            i["network"] == "bluesky" and i["state"] == "failed"
            for i in status["identities"]
        ):
            return status

    before = wait(partial, "partial automatic signup")
    ap_id = next(i["id"] for i in before["identities"] if i["network"] == "mastodon")
    compose("restart", "app", log=True)
    wait(ready, "restarted app")
    compose("up", "-d", "--no-deps", "pds", log=True)

    def enrolled():
        status = api("hosting")
        return (
            status
            if status.get("enrollment", {}).get("state") == "active"
            and len(status["identities"]) == 2
            else None
        )

    identities = wait(enrolled, "native signup recovery")
    assert (
        next(i["id"] for i in identities["identities"] if i["network"] == "mastodon")
        == ap_id
    )
    for _ in range(2):
        api("hosting/retry", {}, method="POST")
    connections = api("connections")["items"]
    assert len(connections) == 2 and all(c["hosted"] for c in connections)
    assert (
        pg("SELECT count(*) FROM hosted_identity WHERE owner_id='" + owner + "'") == "2"
    )
    app_id = compose("ps", "-q", "app").decode().strip()
    info = json.loads(docker("inspect", app_id))[0]
    assert (
        info["Config"]["User"] in ["65532", "65532:65532"]
        and info["HostConfig"]["ReadonlyRootfs"]
    )
    assert "ALL" in info["HostConfig"]["CapDrop"]
    # A valid PNG fixture traverses the same raw upload and image parser as production.
    png = bytes.fromhex("89504e470d0a1a0a")
    import struct, zlib

    def chunk(kind, data):
        return (
            struct.pack(">I", len(data))
            + kind
            + data
            + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
        )

    png += (
        chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 2, 8, 6, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress((b"\0" + b"\0\x85\xf3\xff" * 2) * 2))
        + chunk(b"IEND", b"")
    )
    media_raw = api("media", raw=png, method="PUT")
    media = json.loads(media_raw)
    with client.open(base + "/api/v1/media/" + media["id"]) as r:
        canonical = r.read()
    draft = api(
        "drafts",
        {
            "text": "Container native publication fixture",
            "destinations": [c["id"] for c in connections],
            "mediaIds": [media["id"]],
            "timezone": "UTC",
        },
    )
    api("drafts/" + draft["id"] + "/publish", {})

    def delivered():
        dest = [d for d in api("deliveries")["items"] if d["draftId"] == draft["id"]]
        return (
            dest
            if len(dest) == 2 and all(d["state"] == "published" for d in dest)
            else None
        )

    destinations = wait(delivered, "publication on both native servers")
    feed = api("feed?scope=discover")["items"]
    posts = [p for p in feed if p["text"] == "Container native publication fixture"]
    assert len(posts) == 2
    bsky = next(p for p in posts if p["network"] == "bluesky")
    assert native_blob(bsky["images"][0]) == canonical
    assert native_record(bsky)
    if using_baseline:
        using_baseline = False
        compose("up", "-d", "--no-deps", "--force-recreate", "pds", "plc", log=True)
    compose("restart", "app", "activitypub", "pds", log=True)
    wait(ready, "persistent restart")
    wait(
        lambda: len(
            [
                p
                for p in api("feed?scope=discover")["items"]
                if p["text"] == "Container native publication fixture"
            ]
        )
        == 2,
        "retained source posts",
    )
    wait(
        lambda: native_record(bsky),
        "native repository URI/CID after restart or upgrade",
    )
    assert native_blob(bsky["images"][0]) == canonical
    assert {c["id"] for c in api("connections")["items"]} == {
        c["id"] for c in connections
    }
    # Quiesce every application/native writer before the backup boundary.
    compose("stop", "app", "activitypub", "pds", "plc", log=True)
    expected = snapshot_counts()
    spec = importlib.util.spec_from_file_location(
        "snapshot", ROOT / "scripts/backup-instance.py"
    )
    backup = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(backup)
    with tempfile.TemporaryDirectory(dir=work) as scratch:
        scratch = pathlib.Path(scratch)
        paths = {"configuration": str(env_file)}
        for database in ["zing", "zing_plc"]:
            dump = scratch / (database + ".dump")
            dump.write_bytes(
                compose(
                    "exec",
                    "-T",
                    "postgres",
                    "pg_dump",
                    "-U",
                    "zing",
                    "-d",
                    database,
                    "-Fc",
                    "--no-owner",
                    "--no-acl",
                )
            )
            paths[database] = str(dump)
        for alias, service, destination in [
            ("activitypub", "activitypub", "/data"),
            ("pds", "pds", "/pds"),
            ("storage", "app", "/app/storage"),
            ("owner-claim", "app", "/app/config/owner-claim"),
        ]:
            volume = volume_name(service, destination)
            file = scratch / (alias + ".tar")
            file.write_bytes(
                docker(
                    "run",
                    "--rm",
                    "--network",
                    "none",
                    "-v",
                    volume + ":/snapshot:ro",
                    "python:3.12-slim",
                    "tar",
                    "-cf",
                    "-",
                    "-C",
                    "/snapshot",
                    ".",
                )
            )
            paths[alias] = str(file)
        identity = work / "recovery.agekey"
        backup.run(["age-keygen", "-o", str(identity)])
        recipient = backup.run(["age-keygen", "-y", str(identity)]).decode().strip()
        archive = work / "snapshot.tar.gz.age"
        inventory = backup.backup(
            {"databases": {}, "paths": paths}, archive, recipient, True
        )
        (work / "backup-inventory.json").write_text(
            json.dumps(inventory, indent=2) + "\n"
        )
        restored = scratch / "restored"
        backup.restore(archive, identity, restored, inventory["sha256"])
        recovered_env = work / "recovered.env"
        recovered_env.write_bytes((restored / "paths/configuration").read_bytes())
        os.chmod(recovered_env, 0o600)
        assert recovered_env.read_bytes() == env_file.read_bytes()
        env_file = recovered_env
        env["ZING_TEST_ENV_FILE"] = str(env_file)
        overrides = {
            "services": {"app": {"environment": {"JOBS_WORKERS": "0"}}},
            "volumes": {},
        }
        sqlite_checked = {}
        for alias in ["activitypub", "pds", "storage", "owner-claim"]:
            volume = recovery_project + "-" + alias
            docker("volume", "create", volume)
            cloned_volumes.append(volume)
            docker(
                "run",
                "--rm",
                "--network",
                "none",
                "-i",
                "-v",
                volume + ":/snapshot",
                "python:3.12-slim",
                "tar",
                "-xf",
                "-",
                "--numeric-owner",
                "-C",
                "/snapshot",
                input=(restored / "paths" / alias).read_bytes(),
            )
            if alias in ["activitypub", "pds"]:
                check_sqlite = r"""import pathlib,sqlite3
count=0
for path in pathlib.Path('/snapshot').rglob('*'):
    if path.is_file():
        with path.open('rb') as stream: header=stream.read(16)
        if header==b'SQLite format 3\x00':
            with sqlite3.connect(path.as_uri()+'?mode=ro',uri=True) as db:
                assert db.execute('PRAGMA integrity_check').fetchall()==[('ok',)]
            count+=1
print(count)
"""
                sqlite_checked[alias] = int(
                    docker(
                        "run",
                        "--rm",
                        "--network",
                        "none",
                        "-v",
                        volume + ":/snapshot",
                        "python:3.12-slim",
                        "python3",
                        "-c",
                        check_sqlite,
                    )
                )
            overrides["volumes"][alias] = {"name": volume}
        assert sqlite_checked["activitypub"] > 0 and sqlite_checked["pds"] >= 3
        # JSON is a YAML subset and avoids a YAML dependency in this operator tool.
        clone_override.write_text(json.dumps(overrides))
        compose(
            "up",
            "-d",
            "postgres",
            "valkey",
            "mail",
            clone=True,
            extra=clone_override,
            log=True,
        )
        wait(lambda: postgres_ready(True), "restored PostgreSQL")
        compose(
            "exec",
            "-T",
            "postgres",
            "createdb",
            "-U",
            "zing",
            "zing_plc",
            clone=True,
            extra=clone_override,
            log=True,
        )
        for database in ["zing", "zing_plc"]:
            compose(
                "exec",
                "-T",
                "postgres",
                "pg_restore",
                "-U",
                "zing",
                "-d",
                database,
                "--no-owner",
                "--no-acl",
                "--exit-on-error",
                clone=True,
                extra=clone_override,
                input=(restored / "paths" / database).read_bytes(),
                log=True,
            )
        assert snapshot_counts(True) == expected
        recovering = True
        compose("up", "-d", clone=True, extra=clone_override, log=True)
        wait(ready, "restored app")
        assert {c["id"] for c in api("connections")["items"]} == {
            c["id"] for c in connections
        }
        restored_posts = [
            p
            for p in api("feed?scope=discover")["items"]
            if p["text"] == "Container native publication fixture"
        ]
        assert {p["id"] for p in restored_posts} == {p["id"] for p in posts}
        restored_bsky = next(p for p in restored_posts if p["network"] == "bluesky")
        assert native_blob(restored_bsky["images"][0], True) == canonical
        assert native_record(restored_bsky, True)
        with client.open(base + "/api/v1/media/" + media["id"]) as r:
            assert r.read() == canonical
        assert pg("SELECT count(*) FROM job WHERE state='running'", True) == "0"
        report = {
            "status": "ok",
            "applicationNonRoot": True,
            "applicationRootReadOnly": True,
            "capabilitiesDropped": True,
            "realSMTPVerificationRequired": True,
            "signupCreatesBothNativeIdentities": True,
            "partialSignupSurvivesAppRestart": True,
            "duplicateIdentityPrevention": True,
            "nativeMediaPublishing": True,
            "persistentRestart": True,
            "glibcToMuslUpgradePreservesNativeData": bool(args.baseline_pds_image),
            "encryptedBackupAndFreshVolumeRestore": True,
            "restoredDatabaseCountsMatch": True,
            "allTablesInBothDatabasesCompared": True,
            "nativeSQLiteStoresChecked": sum(sqlite_checked.values()),
            "nativePostIdsAndBlobBytesPreserved": True,
            "nativeRepositoryURIsAndCIDsVerifiedDirectly": True,
            "privateMediaAccessPreserved": True,
            "restoredWorkersDisabled": True,
        }
        (work / "validation.json").write_text(json.dumps(report, indent=2) + "\n")
        (ROOT / ".lidza/container-validation.json").write_text(
            json.dumps(report, indent=2) + "\n"
        )
        print(json.dumps(report))
except Exception:
    (work / "failure.log").write_text(traceback.format_exc())
    try:
        compose(
            "logs",
            "--no-color",
            "--tail",
            "150",
            clone=recovering,
            extra=clone_override if recovering and clone_override.exists() else None,
            log=True,
        )
    except RuntimeError:
        pass
    raise
finally:
    bridge.close()
    for clone in [True, False]:
        try:
            compose(
                "down",
                "--volumes",
                "--remove-orphans",
                clone=clone,
                extra=clone_override if clone and clone_override.exists() else None,
                log=True,
            )
        except RuntimeError:
            pass
    for volume in cloned_volumes:
        try:
            docker("volume", "rm", volume)
        except RuntimeError:
            pass
