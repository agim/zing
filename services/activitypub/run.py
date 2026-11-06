#!/usr/bin/env python3
"""Container supervisor for the upstream server and private provisioning agent."""
import json, os, signal, subprocess, time
from pathlib import Path

root = Path("/data")
root.mkdir(exist_ok=True)
domain = os.environ["ZING_ACTIVITYPUB_DOMAIN"]
protocol = os.environ.get("ZING_ACTIVITYPUB_PROTOCOL", "https")
if protocol not in ("http", "https"):
    raise SystemExit("Invalid ActivityPub protocol")
config = {
    "host": domain,
    "protocol": protocol,
    "bind-address": "0.0.0.0",
    "port": 8080,
    "db-type": "sqlite",
    "db-address": "/data/gotosocial.sqlite",
    "storage-local-base-path": "/data/media",
    "web-template-base-dir": "/gotosocial/web/template",
    "web-asset-base-dir": "/gotosocial/web/assets",
    "accounts-registration-open": False,
    "log-level": "warn",
}
Path("/data/config.json").write_text(json.dumps(config))
os.environ.update(
    GTS_CONFIG_PATH="/data/config.json",
    GTS_BINARY="/gotosocial/gotosocial",
    ZING_AGENT_DATA="/data/agent",
    ZING_ACTIVITYPUB_URL=protocol + "://" + domain,
    ZING_AGENT_BIND="0.0.0.0",
)
children = []


def stop(*_):
    raise KeyboardInterrupt


signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
try:
    children.append(
        subprocess.Popen(
            [
                "/gotosocial/gotosocial",
                "--config-path",
                "/data/config.json",
                "server",
                "start",
            ],
            env=dict(os.environ, GIN_MODE="release"),
        )
    )
    children.append(subprocess.Popen(["python3", "/service/agent.py"]))
    while all(p.poll() is None for p in children):
        request = root / "restart-request"
        ack = root / "restart-ack"
        if request.exists() and (
            not ack.exists() or ack.read_text() != request.read_text()
        ):
            nonce = request.read_text()
            old = children[0]
            old.terminate()
            old.wait(timeout=20)
            children[0] = subprocess.Popen(
                [
                    "/gotosocial/gotosocial",
                    "--config-path",
                    "/data/config.json",
                    "server",
                    "start",
                ],
                env=dict(os.environ, GIN_MODE="release"),
            )
            import urllib.request

            for _ in range(100):
                try:
                    with urllib.request.urlopen(
                        "http://127.0.0.1:8080/readyz", timeout=1
                    ) as response:
                        if response.status == 200:
                            break
                except OSError:
                    time.sleep(0.2)
            else:
                raise SystemExit("ActivityPub restart failed readiness")
            ack.write_text(nonce)
            os.chmod(ack, 0o600)
        time.sleep(0.5)
    raise SystemExit("An ActivityPub service stopped")
except KeyboardInterrupt:
    pass
finally:
    for p in children:
        if p.poll() is None:
            p.terminate()
    for p in children:
        try:
            p.wait(timeout=20)
        except subprocess.TimeoutExpired:
            p.kill()
            p.wait()
