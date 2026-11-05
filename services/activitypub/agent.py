#!/usr/bin/env python3
"""Private provisioning agent for the pinned GoToSocial server. Never publish this port."""
import hmac
import http.cookiejar
import json
import os
from pathlib import Path
import re
import subprocess
import threading
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOCK = threading.Lock()
ROOT = Path(os.environ["ZING_AGENT_DATA"])
ROOT.mkdir(parents=True, exist_ok=True, mode=0o700)
ORIGIN = os.environ["ZING_ACTIVITYPUB_URL"].rstrip("/")
TOKEN = os.environ["ZING_AGENT_TOKEN"]
BINARY = os.environ.get("GTS_BINARY", "/gotosocial/gotosocial")
CONFIG = os.environ["GTS_CONFIG_PATH"]


class Conflict(Exception):
    pass


class SameOrigin(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        u = urllib.parse.urlsplit(newurl)
        if (u.scheme, u.netloc) != tuple(urllib.parse.urlsplit(ORIGIN)[:2]):
            raise RuntimeError("unexpected backend redirect")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def command(action, data):
    args = [
        BINARY,
        "--config-path",
        CONFIG,
        "admin",
        "account",
        action,
        "--username",
        data["username"],
    ]
    if action == "create":
        args.extend(["--email", data["email"], "--password", data["password"]])
    # Upstream CLI requires password flags. Run only in the isolated backend
    # service; never use a shell, forward output, or reuse the Zing password.
    result = subprocess.run(
        args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=45
    )
    if result.returncode:
        raise Conflict("Backend account needs operator reconciliation")


def provision(data):
    if not re.fullmatch(r"[a-z0-9_]{1,30}", data.get("username", "")):
        raise ValueError("Invalid username")
    if not re.fullmatch(r"[0-9a-f-]{36}", data.get("id", "")):
        raise ValueError("Invalid operation ID")
    if (
        not isinstance(data.get("email"), str)
        or "@" not in data["email"]
        or len(data["email"]) > 254
    ):
        raise ValueError("Invalid email")
    if (
        not isinstance(data.get("password"), str)
        or not 24 <= len(data["password"]) <= 128
    ):
        raise ValueError("Invalid service password")
    path = ROOT / (data["id"] + ".json")
    with LOCK:
        identity = {"username": data["username"], "email": data["email"]}
        if path.exists():
            saved = json.loads(path.read_text())
            if any(saved[k] != v for k, v in identity.items()):
                raise Conflict("Operation identity changed")
            if saved["state"] == "creating":
                raise Conflict(
                    "An interrupted creation requires operator reconciliation"
                )
        else:
            fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            with os.fdopen(fd, "w") as f:
                json.dump(dict(identity, state="creating"), f)
                f.flush()
                os.fsync(f.fileno())
            command("create", data)
            tmp = path.with_suffix(".tmp")
            fd = os.open(tmp, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
            with os.fdopen(fd, "w") as f:
                json.dump(dict(identity, state="created"), f)
                f.flush()
                os.fsync(f.fileno())
            os.replace(tmp, path)
        command("confirm", data)
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()), SameOrigin()
    )

    def call(path, body=None, form=False):
        raw = (
            urllib.parse.urlencode(body).encode()
            if form
            else json.dumps(body).encode() if body is not None else None
        )
        req = urllib.request.Request(
            ORIGIN + path,
            data=raw,
            headers={
                "Content-Type": (
                    "application/x-www-form-urlencoded" if form else "application/json"
                )
            },
        )
        with opener.open(req, timeout=20) as r:
            return r.read().decode()

    scope = "read write follow"
    app = json.loads(
        call(
            "/api/v1/apps",
            {
                "client_name": "Zing hosted identity",
                "redirect_uris": "urn:ietf:wg:oauth:2.0:oob",
                "scopes": scope,
            },
        )
    )
    call(
        "/oauth/authorize?"
        + urllib.parse.urlencode(
            {
                "client_id": app["client_id"],
                "redirect_uri": "urn:ietf:wg:oauth:2.0:oob",
                "response_type": "code",
                "scope": scope,
            }
        )
    )
    call(
        "/auth/sign_in", {"username": data["email"], "password": data["password"]}, True
    )
    html = call("/oauth/authorize", {}, True)
    codes = re.findall(r"<code>([^<]+)</code>", html)
    if not codes:
        raise RuntimeError("Backend authorization did not complete")
    token = json.loads(
        call(
            "/oauth/token",
            dict(
                client_id=app["client_id"],
                client_secret=app["client_secret"],
                grant_type="authorization_code",
                code=codes[-1],
                redirect_uri="urn:ietf:wg:oauth:2.0:oob",
            ),
        )
    )
    req = urllib.request.Request(
        ORIGIN + "/api/v1/accounts/verify_credentials",
        headers={"Authorization": "Bearer " + token["access_token"]},
    )
    with opener.open(req, timeout=20) as r:
        account = json.load(r)
    return {
        "subject": account["id"],
        "handle": account["acct"],
        "access": token["access_token"],
    }


class Agent(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        if self.path != "/health":
            self.send_error(404)
            return
        self.respond(200, {"ok": True})

    def respond(self, status, body):
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_POST(self):
        if self.path != "/provision":
            self.send_error(404)
            return
        if not hmac.compare_digest(
            self.headers.get("Authorization", ""), "Bearer " + TOKEN
        ):
            self.respond(401, {"error": "Unauthorized"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 4096:
                raise ValueError("Invalid payload size")
            data = json.loads(self.rfile.read(size))
            result = provision(data)
            self.respond(200, result)
        except (ValueError, KeyError, TypeError):
            self.respond(422, {"error": "Invalid provisioning request"})
        except Conflict:
            self.respond(
                409,
                {
                    "error": "Username unavailable or creation requires operator reconciliation"
                },
            )
        except Exception:
            self.respond(
                502,
                {
                    "error": "Hosted account could not be provisioned; retry with the same operation"
                },
            )


if __name__ == "__main__":
    bind = os.environ.get("ZING_AGENT_BIND", "127.0.0.1")
    ThreadingHTTPServer(
        (bind, int(os.environ.get("ZING_AGENT_PORT", "8091"))), Agent
    ).serve_forever()
