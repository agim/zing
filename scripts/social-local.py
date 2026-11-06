#!/usr/bin/env python3
"""Run pinned social servers locally, with real persistent repositories and an isolated PLC."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import tarfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
DATA = ROOT / ".lidza/social"
RUNTIME = ROOT / ".lidza/runtime/gotosocial"
VERSION = "0.22.3"
ARCHIVE = "gotosocial_0.22.3_linux_amd64.tar.gz"
SHA256 = "10ae814b5389aaaf6d1e2520fd2c61767a26b1a2c000a5c02f693d2f79ca448a"


def private_json(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(value, f, indent=2)


def install():
    if os.uname().machine != "x86_64" or os.uname().sysname != "Linux":
        raise SystemExit(
            "Native installer currently supports Linux amd64. Use the container stack on other hosts."
        )
    if not (RUNTIME / "gotosocial").exists():
        url = f"https://codeberg.org/superseriousbusiness/gotosocial/releases/download/v{VERSION}/{ARCHIVE}"
        blob = urllib.request.urlopen(url, timeout=60).read()
        if hashlib.sha256(blob).hexdigest() != SHA256:
            raise SystemExit("GoToSocial archive checksum mismatch")
        RUNTIME.mkdir(parents=True, exist_ok=True)
        with tarfile.open(fileobj=io.BytesIO(blob), mode="r:gz") as archive:
            archive.extractall(RUNTIME, filter="data")
    installed_version = subprocess.check_output(
        [str(RUNTIME / "gotosocial"), "--version"], text=True
    )
    if "0.22.3-zing-security.1" not in installed_version:
        subprocess.run(
            ["python3", str(ROOT / "scripts/build-activitypub.py")], check=True
        )
    subprocess.run(
        ["corepack", "pnpm", "install", "--frozen-lockfile"],
        cwd=ROOT / "services/pds",
        check=True,
    )
    subprocess.run(
        ["corepack", "pnpm", "install", "--prod", "--frozen-lockfile"],
        cwd=ROOT / "services/dev-plc",
        check=True,
    )


def main():
    os.chdir(ROOT)
    parser = argparse.ArgumentParser()
    parser.add_argument("--install", action="store_true")
    parser.add_argument("--configure", action="store_true")
    parser.add_argument("--peer", action="store_true")
    args = parser.parse_args()
    DATA.mkdir(parents=True, exist_ok=True, mode=0o700)
    if args.install:
        install()
    if not (RUNTIME / "gotosocial").exists():
        raise SystemExit("Run scripts/social-local.py --install first")
    secret_path = DATA / "secrets.json"
    if not secret_path.exists():
        private_json(
            secret_path,
            {
                k: secrets.token_hex(32)
                for k in ["agent", "pdsAdmin", "pdsJwt", "pdsRotation"]
            },
        )
    keys = json.loads(secret_path.read_text())
    for name in ["gotosocial", "pds", "agent"]:
        (DATA / name).mkdir(exist_ok=True, mode=0o700)
    config = DATA / "gotosocial.json"

    def make_config(name, port):
        path = DATA / (name + ".json")
        directory = DATA / name
        directory.mkdir(exist_ok=True, mode=0o700)
        if not path.exists():
            private_json(
                path,
                {
                    "host": f"127.0.0.1:{port}",
                    "protocol": "http",
                    "bind-address": "127.0.0.1",
                    "port": port,
                    "db-type": "sqlite",
                    "db-address": str(directory / "sqlite.db"),
                    "storage-local-base-path": str(directory / "storage"),
                    "web-template-base-dir": str(RUNTIME / "web/template"),
                    "web-asset-base-dir": str(RUNTIME / "web/assets"),
                    "accounts-registration-open": False,
                    "accounts-reason-required": False,
                    "log-level": "warn",
                    "http-client-allow-ips": ["127.0.0.0/8"],
                },
            )
        return path

    config = make_config("gotosocial", 8081)
    if args.configure:
        settings = {
            "ZING_ACTIVITYPUB_URL": "http://127.0.0.1:8081",
            "ZING_ACTIVITYPUB_AGENT_URL": "http://127.0.0.1:8091",
            "ZING_ACTIVITYPUB_AGENT_TOKEN": keys["agent"],
            "ZING_PDS_URL": "http://127.0.0.1:2583",
            "ZING_PDS_ADMIN_PASSWORD": keys["pdsAdmin"],
            "ZING_PDS_HANDLE_DOMAIN": "test",
            "ZING_PDS_READ_MODE": "local",
        }
        subprocess.run(
            ["lidza", "settings", "set"] + [k + "=" + v for k, v in settings.items()],
            env=dict(os.environ, LIDZA_MODE="dev"),
            check=True,
            stdout=subprocess.DEVNULL,
        )
    plc_database = os.environ.get(
        "ZING_PLC_DATABASE_URL", "postgres:///zing_plc?host=/var/run/postgresql"
    )
    probe = subprocess.run(
        ["psql", plc_database, "-Atqc", "SELECT 1"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    if probe.returncode:
        if plc_database != "postgres:///zing_plc?host=/var/run/postgresql":
            raise SystemExit("Create the configured PLC database first")
        subprocess.run(["createdb", "zing_plc"], check=True)
    processes = []
    logs = []

    def start(name, cmd, extra):
        path = DATA / (name + ".log")
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        log = os.fdopen(fd, "ab")
        logs.append(log)
        p = subprocess.Popen(cmd, env=dict(os.environ, **extra), stdout=log, stderr=log)
        processes.append(p)
        return p

    def stop(*_):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        start(
            "plc",
            ["node", "services/dev-plc/start.mjs"],
            {"ZING_PLC_DATABASE_URL": plc_database},
        )
        gts = start(
            "gotosocial",
            [
                str(RUNTIME / "gotosocial"),
                "--config-path",
                str(config),
                "server",
                "start",
            ],
            {"GIN_MODE": "release"},
        )
        start(
            "agent",
            ["python3", "services/activitypub/agent.py"],
            {
                "ZING_AGENT_DATA": str(DATA / "agent"),
                "ZING_AGENT_TOKEN": keys["agent"],
                "ZING_ACTIVITYPUB_URL": "http://127.0.0.1:8081",
                "GTS_BINARY": str(RUNTIME / "gotosocial"),
                "GTS_CONFIG_PATH": str(config),
            },
        )
        pds_env = {
            "PDS_HOSTNAME": "localhost",
            "PDS_PORT": "2583",
            "PDS_DATA_DIRECTORY": str(DATA / "pds"),
            "PDS_BLOBSTORE_DISK_LOCATION": str(DATA / "pds/blobs"),
            "PDS_DID_PLC_URL": "http://127.0.0.1:2582",
            "PDS_JWT_SECRET": keys["pdsJwt"],
            "PDS_ADMIN_PASSWORD": keys["pdsAdmin"],
            "PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX": keys["pdsRotation"],
            "PDS_DEV_MODE": "true",
            "PDS_INVITE_REQUIRED": "true",
            "PDS_SERVICE_HANDLE_DOMAINS": ".test",
            "LOG_ENABLED": "false",
            "ZING_BIND_LOOPBACK": "true",
        }
        start("pds", ["node", "services/pds/start.mjs"], pds_env)
        if args.peer:
            start(
                "peer",
                [
                    str(RUNTIME / "gotosocial"),
                    "--config-path",
                    str(make_config("peer", 8082)),
                    "server",
                    "start",
                ],
                {"GIN_MODE": "release"},
            )
        for _ in range(100):
            if any(p.poll() is not None for p in processes):
                raise SystemExit(
                    "A social service stopped; inspect .lidza/social/*.log"
                )
            try:
                for endpoint in [
                    "http://127.0.0.1:2582/_health",
                    "http://127.0.0.1:8081/readyz",
                    "http://127.0.0.1:8091/health",
                    "http://127.0.0.1:2583/xrpc/_health",
                ]:
                    with urllib.request.urlopen(endpoint, timeout=1) as r:
                        if r.status != 200:
                            raise OSError("not ready")
                break
            except OSError:
                time.sleep(0.2)
        else:
            raise SystemExit("Social services did not become ready")
        print(
            "Hosted servers ready: ActivityPub :8081, PDS :2583, isolated PLC :2582. Start Zing with ./scripts/local.sh.",
            flush=True,
        )
        while all(p.poll() is None for p in processes):
            request = DATA / "restart-request"
            ack = DATA / "restart-ack"
            if request.exists() and (
                not ack.exists() or ack.read_text() != request.read_text()
            ):
                nonce = request.read_text()
                gts.terminate()
                gts.wait(timeout=20)
                processes.remove(gts)
                gts = start(
                    "gotosocial",
                    [
                        str(RUNTIME / "gotosocial"),
                        "--config-path",
                        str(config),
                        "server",
                        "start",
                    ],
                    {"GIN_MODE": "release"},
                )
                for _ in range(100):
                    try:
                        with urllib.request.urlopen(
                            "http://127.0.0.1:8081/readyz", timeout=1
                        ) as response:
                            if response.status == 200:
                                break
                    except OSError:
                        time.sleep(0.2)
                else:
                    raise SystemExit("ActivityPub restart did not become ready")
                ack.write_text(nonce)
                os.chmod(ack, 0o600)
            time.sleep(0.5)
        raise SystemExit("A social service stopped; inspect .lidza/social/*.log")
    except KeyboardInterrupt:
        pass
    finally:
        for p in reversed(processes):
            if p.poll() is None:
                p.terminate()
        for p in reversed(processes):
            try:
                p.wait(timeout=15)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
        for log in logs:
            log.close()


if __name__ == "__main__":
    main()
