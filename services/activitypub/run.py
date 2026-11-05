#!/usr/bin/env python3
"""Container supervisor for the upstream server and private provisioning agent."""
import json, os, signal, subprocess, time
from pathlib import Path

root = Path("/data")
root.mkdir(exist_ok=True)
domain = os.environ["ZING_ACTIVITYPUB_DOMAIN"]
config = {
    "host": domain,
    "protocol": "https",
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
    ZING_ACTIVITYPUB_URL="https://" + domain,
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
