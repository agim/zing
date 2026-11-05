#!/usr/bin/env python3
"""Opt-in real local native-server/chat/moderation acceptance. Removes its fixture user."""
import json, os, re, secrets, subprocess, time, uuid, struct, zlib
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent
os.chdir(ROOT)
access = json.loads((ROOT / ".lidza/local-access.json").read_text())
base = access["url"]
assert urlsplit(base).hostname in ["127.0.0.1", "localhost"], "Local-only acceptance"
env = dict(os.environ, LIDZA_MODE="dev")


def setting(*args):
    return subprocess.run(
        ["lidza", "settings", *args], env=env, capture_output=True, text=True
    )


previous = setting("get", "ZING_ALLOWED_EMAILS")
email = "community-" + uuid.uuid4().hex[:12] + "@zing.local"
password = secrets.token_urlsafe(24)


def set_policy(value):
    with sync_playwright() as engine:
        request = engine.request.new_context(base_url=base)
        response = request.post(
            "/api/v1/auth/login",
            data={"email": access["email"], "password": access["password"]},
        )
        assert response.ok, "Operator login"
        response = request.post(
            "/admin/settings",
            form={"section": "zing-instance", "ZING_ALLOWED_EMAILS": value},
            headers={"Origin": base, "Sec-Fetch-Site": "same-origin"},
        )
        assert response.ok, "Private policy update"
        request.dispose()


set_policy(
    ",".join(
        x
        for x in [previous.stdout.strip() if previous.returncode == 0 else "", email]
        if x
    )
)


# env.Values caches settings briefly. Always restore the previous private policy.
def wait(fn, label, seconds=60):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        result = fn()
        if result:
            return result
        time.sleep(0.3)
    raise AssertionError(label)


def sql(query):
    return subprocess.check_output(
        ["psql", "postgres:///zing_dev?host=/var/run/postgresql", "-Atc", query],
        text=True,
    ).strip()


def check(response):
    assert response.ok, "HTTP " + str(response.status)
    return response.json() if response.status != 204 else None


try:
    time.sleep(4)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        operator = browser.new_context()
        owner = operator.new_page()
        owner.goto(base + "/login")
        owner.get_by_label("Email", exact=True).fill(access["email"])
        owner.get_by_label("Password", exact=True).fill(access["password"])
        owner.get_by_role("button", name="Continue", exact=True).click()
        expect(
            owner.get_by_role("button", name="Settings", exact=True).first
        ).to_be_visible()
        user = browser.new_context()
        client = user.request
        check(
            client.post(
                base + "/api/v1/auth/register",
                data={
                    "email": email,
                    "password": password,
                    "name": "Community acceptance fixture",
                },
            )
        )

        def verification():
            messages = client.get("http://127.0.0.1:8025/messages").json()
            for message in messages:
                if email in message["to"]:
                    match = re.search(r"[?&]token=([A-Za-z0-9_-]+)", message["text"])
                    if match:
                        return match[1]

        token = wait(verification, "SMTP verification")
        check(client.post(base + "/api/v1/auth/verify", data={"token": token}))
        subject = check(client.get(base + "/api/v1/auth/session"))["user"]["subject"]
        for network in ["bluesky", "mastodon"]:
            check(
                client.post(
                    base + "/api/v1/hosting/identities",
                    data={
                        "network": network,
                        "username": "verify" + uuid.uuid4().hex[:12],
                    },
                    timeout=60000,
                )
            )
        connections = check(client.get(base + "/api/v1/connections"))["items"]
        ours = check(operator.request.get(base + "/api/v1/connections"))["items"]
        a = next(c for c in ours if c["hosted"] and c["network"] == "bluesky")
        b = next(c for c in connections if c["network"] == "bluesky")
        assert (
            "zing-chat" in b["capabilities"]
            and "conversations" not in b["capabilities"]
        )

        def chunk(name, data):
            return (
                struct.pack(">I", len(data))
                + name
                + data
                + struct.pack(">I", zlib.crc32(name + data) & 0xFFFFFFFF)
            )

        png = (
            b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 2, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress((b"\x00" + b"\x00\x85\xf3\xff" * 2) * 2))
            + chunk(b"IEND", b"")
        )
        media = check(
            client.put(
                base + "/api/v1/media",
                data=png,
                headers={
                    "Content-Type": "image/png",
                    "Origin": base,
                    "Sec-Fetch-Site": "same-origin",
                },
            )
        )
        canonical = client.get(base + "/api/v1/media/" + media["id"]).body()
        draft = check(
            client.post(
                base + "/api/v1/drafts",
                data={
                    "text": "Labeled community acceptance post",
                    "destinations": [c["id"] for c in connections],
                    "mediaIds": [media["id"]],
                    "timezone": "UTC",
                },
            )
        )
        check(client.post(base + "/api/v1/drafts/" + draft["id"] + "/publish", data={}))

        def published():
            deliveries = check(client.get(base + "/api/v1/deliveries"))["items"]
            dest = [d for d in deliveries if d["draftId"] == draft["id"]]
            return (
                dest
                if len(dest) == 2 and all(d["state"] == "published" for d in dest)
                else None
            )

        delivery = wait(published, "Native multi-server publication")
        feed = check(
            operator.request.get(base + "/api/v1/feed?network=bluesky&scope=discover")
        )
        post = next(
            x for x in feed["items"] if x["text"] == "Labeled community acceptance post"
        )
        assert len(post["images"]) == 1, "Published PDS blob is absent from the index"
        assert (
            operator.request.get(post["images"][0]).body() == canonical
        ), "Published blob differs from canonical sanitized media"
        for action in ["follow", "like", "repost", "unlike", "unrepost"]:
            check(
                operator.request.post(
                    base + "/api/v1/actions",
                    data={
                        "connectionId": a["id"],
                        "action": action,
                        "target": b["subject"] if action == "follow" else post["id"],
                        "cid": post["cid"],
                    },
                )
            )
        following = check(
            operator.request.get(base + "/api/v1/feed?network=bluesky&scope=following")
        )
        assert any(x["id"] == post["id"] for x in following["items"])
        convo = check(
            operator.request.post(
                base + "/api/v1/conversations/open",
                data={"connectionId": a["id"], "recipient": b["subject"]},
            )
        )
        assert convo["service"] == "zing" and convo["pending"]
        invitation = {
            "connectionId": a["id"],
            "conversationId": convo["id"],
            "text": "Explicit local chat invitation",
            "clientId": str(uuid.uuid4()),
        }
        check(operator.request.post(base + "/api/v1/messages", data=invitation))
        check(operator.request.post(base + "/api/v1/messages", data=invitation))
        second = dict(invitation, clientId=str(uuid.uuid4()))
        assert (
            operator.request.post(base + "/api/v1/messages", data=second).status == 409
        )
        check(
            client.post(
                base + "/api/v1/conversations/" + convo["id"] + "/accept",
                data={"connectionId": b["id"]},
            )
        )
        check(
            client.post(
                base + "/api/v1/messages",
                data={
                    "connectionId": b["id"],
                    "conversationId": convo["id"],
                    "text": "A private accepted reply",
                    "clientId": str(uuid.uuid4()),
                },
            )
        )
        messages = check(
            operator.request.get(
                base
                + "/api/v1/messages?connectionID="
                + a["id"]
                + "&conversationID="
                + convo["id"]
            )
        )
        assert len(messages["items"]) == 2
        reply = next(m for m in messages["items"] if m["senderDid"] == b["subject"])
        check(
            operator.request.post(
                base + "/api/v1/moderation/reports",
                data={
                    "connectionId": a["id"],
                    "kind": "message",
                    "target": b["subject"],
                    "messageId": reply["id"],
                    "reason": "Labeled selected-message moderation fixture",
                },
            )
        )
        check(
            operator.request.post(
                base + "/api/v1/moderation/reports",
                data={
                    "connectionId": a["id"],
                    "kind": "post",
                    "target": post["id"],
                    "reason": "Labeled native takedown fixture",
                },
            )
        )

        def moderate(kind, target="", report="", network="all"):
            owner.goto(base + "/admin/moderation")
            expect(
                owner.get_by_role("heading", name="Instance moderation")
            ).to_be_visible()
            key = owner.locator("input[name=requestKey]").first.input_value()
            response = operator.request.post(
                base + "/admin/moderation/action",
                form={
                    "requestKey": key,
                    "kind": kind,
                    "network": network,
                    "target": target,
                    "report": report,
                    "reason": "Local acceptance fixture action",
                },
                headers={"Origin": base, "Sec-Fetch-Site": "same-origin"},
                timeout=60000,
            )
            assert response.ok
            action_id = sql(
                "SELECT id FROM moderation_action WHERE request_key='" + key + "'"
            )
            assert action_id, "Moderation action was not recorded"
            wait(
                lambda: sql(
                    "SELECT state FROM moderation_action WHERE id='" + action_id + "'"
                )
                == "applied",
                kind + " native confirmation",
                90,
            )

        report = sql(
            "SELECT id FROM moderation_report WHERE target='"
            + post["id"]
            + "' AND kind='post' ORDER BY created_at DESC LIMIT 1"
        )
        moderate("remove_post", report=report, network="bluesky")
        hidden = check(
            operator.request.get(base + "/api/v1/feed?network=bluesky&scope=discover")
        )
        assert all(x["id"] != post["id"] for x in hidden["items"])
        moderate("suspend", subject)
        assert client.get(base + "/api/v1/connections").status == 403
        status = check(
            client.get(
                "http://127.0.0.1:2583/xrpc/com.atproto.sync.getRepoStatus?did="
                + b["subject"]
            )
        )
        assert not status["active"]
        check(
            client.post(
                base + "/api/v1/moderation/appeal",
                data={"reason": "Labeled appeal fixture"},
            )
        )
        moderate("restore", subject)
        check(client.get(base + "/api/v1/connections"))
        status = check(
            client.get(
                "http://127.0.0.1:2583/xrpc/com.atproto.sync.getRepoStatus?did="
                + b["subject"]
            )
        )
        assert status["active"]
        moderate("block_domain", "acceptance.invalid", network="mastodon")
        moderate("unblock_domain", "acceptance.invalid", network="mastodon")
        view = user.new_page()
        view.goto(base + "/#Messages")
        expect(view.get_by_text("Zing chat", exact=False).first).to_be_visible()
        view.get_by_role(
            "button", name=re.compile(re.escape(a["handle"]))
        ).first.click()
        expect(
            view.locator(".bubble").filter(has_text="A private accepted reply")
        ).to_be_visible()
        expect(view.get_by_role("alert")).to_have_count(0)
        view.screenshot(
            path=str(ROOT / ".lidza/hosting-acceptance/chat.png"), full_page=True
        )
        view.set_viewport_size({"width": 390, "height": 844})
        assert view.evaluate("document.documentElement.scrollWidth <= innerWidth")
        view.screenshot(
            path=str(ROOT / ".lidza/hosting-acceptance/chat-mobile.png"), full_page=True
        )
        # Erasure must work even after both native identities have been disabled.
        moderate("suspend", subject)
        check(client.post(base + "/api/v1/auth/delete", data={"password": password}))
        wait(
            lambda: sql(
                "SELECT count(*) FROM job WHERE kind='zing.delete-hosted' AND payload->>'Subject' IN ('"
                + b["subject"]
                + "','"
                + next(c["subject"] for c in connections if c["network"] == "mastodon")
                + "') AND state<>'done'"
            )
            == "0",
            "Native suspended-account erasure",
            120,
        )
        assert (
            sql("SELECT count(*) FROM chat_message WHERE sender_id='" + subject + "'")
            == "0"
        )
        assert (
            sql("SELECT count(*) FROM at_record WHERE did='" + b["subject"] + "'")
            == "0"
        )
        browser.close()
    print(
        "PASS: real hosted publication/actions/feed, consented private Zing chat, native takedown, suspend/appeal/restore, domain policy, and suspended-account erasure."
    )
finally:
    # Delete the labeled fixture even if an intermediate assertion fails.
    with sync_playwright() as cleanup_engine:
        cleanup = cleanup_engine.request.new_context(base_url=base)
        signed = cleanup.post(
            "/api/v1/auth/login", data={"email": email, "password": password}
        )
        if signed.ok:
            erased = cleanup.post("/api/v1/auth/delete", data={"password": password})
            assert (
                erased.status == 204
            ), "Fixture cleanup must remain visible on failure"
        cleanup.dispose()
    set_policy(previous.stdout.strip() if previous.returncode == 0 else "")
