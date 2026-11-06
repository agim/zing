#!/usr/bin/env python3
"""Review full Trivy reports without suppressing findings or treating residuals as fixes."""
import argparse
import collections
import json
from pathlib import Path
from native_runtime_review import verify_native_sources

ROOT = Path(__file__).resolve().parent.parent
IMAGES = ("app", "activitypub", "pds", "plc", "postgres", "valkey")


def verify(folder, policy):
    reviewed = {
        (r["id"], p["name"], p["version"]): r
        for r in policy["findings"]
        for p in r["packages"]
    }
    report = {
        "status": "ok_with_reviewed_residuals",
        "scannerExclusions": False,
        "images": {},
        "failures": [],
    }
    for name in IMAGES:
        path = folder / (name + ".json")
        if not path.exists():
            report["failures"].append(name + ": missing full image report")
            continue
        scan = json.loads(path.read_text())
        metadata = scan.get("Metadata", {})
        if (
            scan.get("ArtifactType") != "container_image"
            or not metadata.get("ImageID")
            or not scan.get("Results")
        ):
            report["failures"].append(name + ": invalid or incomplete image report")
            continue
        image = metadata.get("ImageConfig", {})
        if (image.get("os"), image.get("architecture")) != ("linux", "amd64"):
            report["failures"].append(name + ": architecture needs a separate review")
        findings = [v for r in scan["Results"] for v in r.get("Vulnerabilities", [])]
        checks = []
        for v in findings:
            key = (v["VulnerabilityID"], v["PkgName"], v["InstalledVersion"])
            entry = reviewed.get(key)
            accepted = (
                entry is not None
                and name in entry["images"]
                and v["Severity"] in entry["severities"]
                and not v.get("FixedVersion")
            )
            if v["Severity"] in ("CRITICAL", "HIGH", "UNKNOWN") or not accepted:
                report["failures"].append(
                    name
                    + ": "
                    + v["VulnerabilityID"]
                    + " "
                    + v["PkgName"]
                    + " requires remediation or review"
                )
            checks.append(
                {
                    "id": v["VulnerabilityID"],
                    "package": v["PkgName"],
                    "installed": v["InstalledVersion"],
                    "severity": v["Severity"],
                    "fixedVersion": v.get("FixedVersion", ""),
                    "disposition": entry["disposition"] if accepted else "unreviewed",
                    "reason": entry["reason"] if accepted else "",
                    "source": entry["source"] if accepted else v.get("PrimaryURL", ""),
                }
            )
        report["images"][name] = {
            "imageId": metadata["ImageID"],
            "counts": dict(collections.Counter(v["Severity"] for v in findings)),
            "remaining": checks,
        }
    if report["failures"]:
        report["status"] = "failed"
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reports", type=Path, required=True)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    policy = json.loads((ROOT / "services/security/runtime-review.json").read_text())
    report = verify(args.reports, policy)
    source_policy = json.loads(
        (ROOT / "services/security/native-runtime-review.json").read_text()
    )
    report["nativeSourceReview"] = verify_native_sources(args.reports, source_policy)
    report["failures"].extend(report["nativeSourceReview"]["failures"])
    from node_backport_review import verify_backports

    backports = json.loads(
        (ROOT / "services/security/node-backports-policy.json").read_text()
    )
    report["localBackportReview"] = verify_backports(args.reports, backports)
    report["failures"].extend(report["localBackportReview"]["failures"])
    if (
        not report["nativeSourceReview"]["failures"]
        and not report["localBackportReview"]["failures"]
    ):
        report["nativeSourceReview"][
            "status"
        ] = "locally_backported_with_vendor_advisories_retained"
        report["nativeSourceReview"]["localDisposition"] = (
            "All four template findings are backported into rebuilt consumers. "
            "The distribution package versions/advisories remain tracked; "
            "they are not evidence of a patched prebuilt Node binary."
        )
    if report["failures"]:
        report["status"] = "failed"
    if args.out:
        args.out.write_text(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {
                "status": report["status"],
                "counts": {k: v["counts"] for k, v in report["images"].items()},
                "failures": report["failures"],
            }
        )
    )
    raise SystemExit(1 if report["failures"] else 0)


if __name__ == "__main__":
    main()
