#!/usr/bin/env python3
"""Check actual patched modules and fail any new/unreviewed service advisory."""
import hashlib, json, pathlib, subprocess, sys

root = pathlib.Path(__file__).resolve().parent.parent
provenance = json.loads((root / "services/vendor/provenance.json").read_text())
for name, expected in provenance["artifacts"].items():
    assert (
        hashlib.sha256((root / "services/vendor" / name).read_bytes()).hexdigest()
        == expected
    ), "Vendor archive checksum changed"
patches = {
    "zod@3.25.76.patch": "4d5d3cae6bc9898a5ad2385d066e7c025f446a49793e6d8178c0388112fdbe3f",
    "deepmerge@4.3.1.patch": "5d4a4389c315f84c61fea91aba67c2e34299c8dbb5d29b569ea9a20dccec08a0",
}
reviewed = {
    "https://github.com/advisories/GHSA-mrhx-c7xc-m3w5": "zod@3.25.76.patch",
    "https://github.com/advisories/GHSA-gcqx-w48m-w2h4": "deepmerge@4.3.1.patch",
}
for service in ["pds", "dev-plc"]:
    folder = root / "services" / service
    subprocess.run(["node", "security-check.mjs"], cwd=folder, check=True)
    result = subprocess.run(
        ["corepack", "pnpm", "audit", "--prod", "--json"],
        cwd=folder,
        capture_output=True,
        text=True,
    )
    audit = json.loads(result.stdout)
    assert result.returncode in (0, 1) and "metadata" in audit, "Audit did not complete"
    alerts = []
    for advisory in audit.get("advisories", {}).values():
        url = advisory["url"]
        assert url in reviewed, "Unreviewed service advisory: " + url
        name = reviewed[url]
        assert (
            hashlib.sha256((folder / "patches" / name).read_bytes()).hexdigest()
            == patches[name]
        ), "Backport content changed; review required"
        alerts.append(
            {
                "advisory": url,
                "severity": advisory["severity"],
                "status": "version alert; regression-tested code backport",
                "patch": name,
            }
        )
    print(
        json.dumps(
            {
                "service": service,
                "unreviewed_advisories": 0,
                "remaining_upstream_version_alerts": alerts,
            }
        )
    )
