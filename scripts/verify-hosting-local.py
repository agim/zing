#!/usr/bin/env python3
"""Opt-in acceptance on a disposable local instance; creates labeled fixture posts."""
from pathlib import Path
import json, re, time
from playwright.sync_api import sync_playwright, expect

repo = Path(__file__).resolve().parent.parent
root = repo
from urllib.parse import urlsplit

artifacts = repo / ".lidza/hosting-acceptance"
artifacts.mkdir(parents=True, exist_ok=True)
a = json.loads((repo / ".lidza/local-access.json").read_text())
base = a["url"]
assert urlsplit(base).hostname in [
    "127.0.0.1",
    "localhost",
], "This acceptance script is local-only"
with sync_playwright() as p:
    b = p.chromium.launch(headless=True)
    page = b.new_page(viewport={"width": 1440, "height": 1000})
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(base + "/login")
    page.get_by_label("Email", exact=True).fill(a["email"])
    page.get_by_label("Password", exact=True).fill(a["password"])
    page.get_by_role("button", name="Continue", exact=True).click()
    expect(
        page.get_by_role("button", name="Settings", exact=True).first
    ).to_be_visible()
    page.get_by_role("button", name="Settings", exact=True).first.click()
    expect(
        page.get_by_role("heading", name="Your hosted identities", exact=True)
    ).to_be_visible()
    for network in ["mastodon", "bluesky"]:
        status = page.request.get(base + "/api/v1/hosting").json()
        assert any(
            n["network"] == network and n["available"] for n in status["networks"]
        )
        for n in status["networks"]:
            if n["available"]:
                assert n["domain"] in [
                    "127.0.0.1:8081",
                    "test",
                ], "Refusing a non-local social backend"
        if network == "bluesky" and any(
            i["network"] == network
            and i["username"] == "operator"
            and i["state"] not in ["active", "rejected"]
            for i in status["identities"]
        ):
            rejected = page.request.post(
                base + "/api/v1/hosting/identities",
                data={"network": "bluesky", "username": "operator"},
            )
            assert rejected.status == 422, rejected.json()
        if not any(
            i["network"] == network and i["state"] == "active"
            for i in status["identities"]
        ):

            page.get_by_label("Hosted network", exact=True).select_option(network)
            page.get_by_label("Username", exact=True).fill(
                "zingoperator" if network == "bluesky" else "operator"
            )
            with page.expect_response(
                lambda r: r.url.endswith("/api/v1/hosting/identities")
                and r.request.method == "POST",
                timeout=60000,
            ) as response:
                page.get_by_role(
                    "button", name="Create hosted identity", exact=True
                ).click()
            assert response.value.status == 200, response.value.json()
            expect(
                page.get_by_text("Your hosted identity is ready.", exact=True)
            ).to_be_visible(timeout=60000)
        conns = page.request.get(base + "/api/v1/connections").json()["items"]
        c = next(c for c in conns if c["hosted"] and c["network"] == network)
        assert urlsplit(c["server"]).hostname in [
            "127.0.0.1",
            "localhost",
        ], "Refusing to publish to an external server"
        text = "Hosted " + network + " acceptance: this post lives on our own server."
        existing = page.request.get(base + "/api/v1/feed?network=" + network).json()
        if any(post["text"] == text for post in existing["items"]):
            print(network, "existing durable hosted post verified")
            continue
        d = page.request.post(
            base + "/api/v1/drafts",
            data={
                "text": text,
                "destinations": [c["id"]],
                "mediaIds": [],
                "timezone": "UTC",
            },
        )
        assert d.ok, d.status
        published = page.request.post(
            base + "/api/v1/drafts/" + d.json()["id"] + "/publish", data={}
        )
        assert published.ok, published.status
        for _ in range(100):
            deliveries = page.request.get(base + "/api/v1/deliveries").json()["items"]
            delivery = next(x for x in deliveries if x["draftId"] == d.json()["id"])
            if delivery["state"] in ["published", "failed", "uncertain"]:
                break
            time.sleep(0.2)
        assert delivery["state"] == "published", delivery
        feed = page.request.get(base + "/api/v1/feed?network=" + network).json()
        assert any(post["text"] == text for post in feed["items"]), feed.get("warnings")
        print(network, "hosted creation and durable publication verified")
    page.get_by_role("button", name="Home", exact=True).first.click()
    expect(
        page.get_by_text(
            "Hosted mastodon acceptance: this post lives on our own server.", exact=True
        ).first
    ).to_be_visible(timeout=15000)
    expect(
        page.get_by_text(
            "Hosted bluesky acceptance: this post lives on our own server.", exact=True
        )
    ).to_be_visible()
    page.screenshot(path=str(artifacts / "home.png"), full_page=True)
    page.set_viewport_size({"width": 390, "height": 844})
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    page.screenshot(path=str(artifacts / "mobile.png"), full_page=True)
    assert not errors, errors
    b.close()
print("Actual local server journey passed for both networks; browser errors: 0")
