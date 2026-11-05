#!/usr/bin/env python3
"""Regression for the version-specific native tombstone privacy supplement."""
import importlib.util, json, os, sqlite3, tempfile, threading, urllib.request, urllib.error, uuid
from pathlib import Path
from http.server import ThreadingHTTPServer

ROOT = Path(__file__).resolve().parent.parent
with tempfile.TemporaryDirectory() as scratch:
    root = Path(scratch)
    db = root / "native.sqlite"
    config = root / "config.json"
    config.write_text(json.dumps({"db-type": "sqlite", "db-address": str(db)}))
    os.environ.update(
        ZING_AGENT_DATA=str(root / "journal"),
        ZING_ACTIVITYPUB_URL="http://127.0.0.1:9",
        ZING_AGENT_TOKEN="test-only-agent-secret",
        GTS_CONFIG_PATH=str(config),
    )
    spec = importlib.util.spec_from_file_location(
        "test_agent", ROOT / "services/activitypub/agent.py"
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    restarts = []
    module.restart_backend = lambda: restarts.append(True)
    connection = sqlite3.connect(db)
    connection.executescript(
        "CREATE TABLE accounts(id TEXT,username TEXT,domain TEXT,suspended_at TEXT,suspension_origin TEXT);CREATE TABLE users(id TEXT,account_id TEXT,email TEXT,unconfirmed_email TEXT);"
    )
    connection.execute("INSERT INTO accounts VALUES('actor','fixture',NULL,NULL,'')")
    connection.execute(
        "INSERT INTO users VALUES('user','actor','private@example.invalid','pending@example.invalid')"
    )
    connection.commit()
    operation = str(uuid.uuid4())
    (module.ROOT / (operation + ".json")).write_text(
        json.dumps(
            {
                "username": "fixture",
                "email": "private@example.invalid",
                "state": "created",
            }
        )
    )
    server = ThreadingHTTPServer(("127.0.0.1", 0), module.Agent)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    def call(body, authorized=True):
        request = urllib.request.Request(
            "http://127.0.0.1:" + str(server.server_port) + "/account-erased",
            data=json.dumps(body).encode(),
            headers={
                "Authorization": "Bearer test-only-agent-secret" if authorized else "",
                "Content-Type": "application/json",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as e:
            return e.code, {}

    body = {"id": operation, "subject": "actor", "username": "fixture"}
    assert call(body, False)[0] == 401
    assert call(dict(body, id=str(uuid.uuid4())))[0] == 422
    assert call(body) == (200, {"erased": False})
    assert (
        connection.execute("SELECT email FROM users").fetchone()[0]
        == "private@example.invalid"
    )
    connection.execute("UPDATE accounts SET suspended_at='2026-11-05T00:00:00Z'")
    connection.commit()
    assert call(body) == (200, {"erased": False})
    connection.execute("UPDATE accounts SET suspension_origin='native-erasure'")
    connection.commit()
    assert call(body) == (200, {"erased": True})
    row = connection.execute("SELECT email,unconfirmed_email FROM users").fetchone()
    assert row == ("deleted+" + operation + "@accounts.invalid", "")
    assert len(restarts) == 1
    assert call(body) == (200, {"erased": True})
    assert len(restarts) == 1
    server.shutdown()
    server.server_close()
    thread.join()
    connection.close()
print(
    "PASS: agent authentication, operation ownership, active-account protection, native tombstone verification, email redaction and idempotency."
)
