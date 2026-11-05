#!/usr/bin/env python3
"""Exercise real signed federation between two isolated TLS GoToSocial servers."""
import importlib.util, json, os, secrets, ssl, subprocess, tempfile, time, urllib.parse, urllib.request, uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
BINARY = ROOT / ".lidza/runtime/gotosocial/gotosocial"


def main():
    os.chdir(ROOT)
    if not BINARY.exists():
        raise SystemExit("Install local social servers first")
    # No identity or TLS state is reused from the user instance.
    with tempfile.TemporaryDirectory(prefix="federation-", dir=ROOT / ".lidza") as temp:
        directory = Path(temp)
        os.chmod(directory, 0o700)
        cert = directory / "cert.pem"
        key = directory / "key.pem"
        subprocess.run(
            [
                "openssl",
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-keyout",
                str(key),
                "-out",
                str(cert),
                "-days",
                "1",
                "-subj",
                "/CN=Zing local federation acceptance",
                "-addext",
                "subjectAltName=IP:127.0.0.1",
                "-addext",
                "basicConstraints=critical,CA:TRUE",
            ],
            check=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        key.chmod(0o600)
        context = ssl.create_default_context(cafile=str(cert))
        processes = []
        logs = []
        old_cert = os.environ.get("SSL_CERT_FILE")
        os.environ["SSL_CERT_FILE"] = str(cert)
        try:
            identities = []
            for name, port in [("alice", 8441), ("bob", 8442)]:
                data = directory / name
                data.mkdir()
                origin = f"https://127.0.0.1:{port}"
                config = {
                    "host": f"127.0.0.1:{port}",
                    "protocol": "https",
                    "bind-address": "127.0.0.1",
                    "port": port,
                    "db-type": "sqlite",
                    "db-address": str(data / "sqlite.db"),
                    "storage-local-base-path": str(data / "storage"),
                    "web-template-base-dir": str(BINARY.parent / "web/template"),
                    "web-asset-base-dir": str(BINARY.parent / "web/assets"),
                    "accounts-registration-open": False,
                    "log-level": "warn",
                    "http-client-allow-ips": ["127.0.0.0/8"],
                    "tls-certificate-chain": str(cert),
                    "tls-certificate-key": str(key),
                }
                config_path = data / "config.json"
                config_path.write_text(json.dumps(config))
                log = open(data / "server.log", "wb")
                logs.append(log)
                processes.append(
                    subprocess.Popen(
                        [
                            str(BINARY),
                            "--config-path",
                            str(config_path),
                            "server",
                            "start",
                        ],
                        env=dict(os.environ, GIN_MODE="release"),
                        stdout=log,
                        stderr=log,
                    )
                )
                for _ in range(100):
                    try:
                        with urllib.request.urlopen(
                            origin + "/readyz", context=context, timeout=1
                        ) as r:
                            if r.status == 200:
                                break
                    except OSError:
                        time.sleep(0.1)
                else:
                    raise RuntimeError("Isolated federation server did not start")
                os.environ.update(
                    ZING_AGENT_DATA=str(data / "agent"),
                    ZING_ACTIVITYPUB_URL=origin,
                    ZING_AGENT_TOKEN=secrets.token_urlsafe(32),
                    GTS_BINARY=str(BINARY),
                    GTS_CONFIG_PATH=str(config_path),
                )
                spec = importlib.util.spec_from_file_location(
                    "zing_agent_" + name, ROOT / "services/activitypub/agent.py"
                )
                agent = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(agent)
                identity = agent.provision(
                    {
                        "id": str(uuid.uuid4()),
                        "username": name,
                        "email": name + "@zing.local",
                        "password": secrets.token_urlsafe(32),
                    }
                )
                identities.append((origin, identity["access"]))

            def call(which, path, body=None, method=None):
                origin, token = identities[which]
                data = json.dumps(body).encode() if body is not None else None
                req = urllib.request.Request(
                    origin + path,
                    data=data,
                    method=method,
                    headers={
                        "Content-Type": "application/json",
                        "Authorization": "Bearer " + token,
                    },
                )
                with urllib.request.urlopen(
                    req, context=context, timeout=45
                ) as response:
                    raw = response.read()
                    return json.loads(raw) if raw else None

            def eventually(fn, message):
                for _ in range(100):
                    result = fn()
                    if result:
                        return result
                    time.sleep(0.2)
                raise AssertionError(message)

            found = call(
                0,
                "/api/v2/search?"
                + urllib.parse.urlencode(
                    {
                        "type": "accounts",
                        "resolve": "true",
                        "q": identities[1][0] + "/users/bob",
                    }
                ),
            )
            assert found["accounts"], "remote actor discovery failed"
            remote = found["accounts"][0]["id"]
            call(0, "/api/v1/accounts/" + remote + "/follow", {})
            requests = eventually(
                lambda: call(1, "/api/v1/follow_requests"),
                "signed Follow was not received",
            )
            call(1, "/api/v1/follow_requests/" + requests[0]["id"] + "/authorize", {})
            eventually(
                lambda: call(0, "/api/v1/accounts/relationships?id[]=" + remote)[0][
                    "following"
                ],
                "signed Accept was not received",
            )
            post = call(
                1,
                "/api/v1/statuses",
                {
                    "status": "Real federation acceptance from the second server.",
                    "visibility": "public",
                },
            )

            def received():
                return next(
                    (
                        p
                        for p in call(0, "/api/v1/timelines/home")
                        if p["uri"] == post["uri"]
                    ),
                    None,
                )

            local = eventually(received, "signed Create was not delivered")
            call(0, "/api/v1/statuses/" + local["id"] + "/favourite", {})
            eventually(
                lambda: any(
                    n["type"] == "favourite" for n in call(1, "/api/v1/notifications")
                ),
                "signed Like was not received",
            )
            call(1, "/api/v1/statuses/" + post["id"], method="DELETE")
            eventually(lambda: not received(), "signed Delete was not delivered")
            print(
                "PASS: real TLS discovery, signed Follow/Accept, Create, Like and Delete between isolated GoToSocial servers. No public network identity was created."
            )
        finally:
            for process in processes:
                if process.poll() is None:
                    process.terminate()
            for process in processes:
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            for log in logs:
                log.close()
            if old_cert is None:
                os.environ.pop("SSL_CERT_FILE", None)
            else:
                os.environ["SSL_CERT_FILE"] = old_cert


if __name__ == "__main__":
    main()
