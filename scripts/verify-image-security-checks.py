#!/usr/bin/env python3
"""Regression: an explicit residual review cannot admit new or actionable findings."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location(
    "image_review", ROOT / "scripts/verify-image-security.py"
)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
policy = json.loads((ROOT / "services/security/runtime-review.json").read_text())
base = {
    "ArtifactType": "container_image",
    "Metadata": {
        "ImageID": "sha256:fixture",
        "ImageConfig": {"os": "linux", "architecture": "amd64"},
    },
    "Results": [{"Vulnerabilities": []}],
}
with tempfile.TemporaryDirectory(prefix="zing-image-review-") as folder:
    root = Path(folder)
    for name in m.IMAGES:
        (root / (name + ".json")).write_text(json.dumps(base))
    entry = policy["findings"][0]
    package = entry["packages"][0]
    finding = {
        "VulnerabilityID": entry["id"],
        "PkgName": package["name"],
        "InstalledVersion": package["version"],
        "Severity": entry["severities"][0],
        "FixedVersion": "",
    }
    pds = copy.deepcopy(base)
    pds["Results"][0]["Vulnerabilities"] = [finding]
    path = root / "pds.json"
    path.write_text(json.dumps(pds))
    assert not m.verify(root, policy)[
        "failures"
    ], "Reviewed exact residual must remain visible without failing"
    for field, value in [
        ("VulnerabilityID", "CVE-2099-UNREVIEWED"),
        ("InstalledVersion", "unreviewed-version"),
        ("Severity", "HIGH"),
        ("FixedVersion", "newly-available-fix"),
    ]:
        changed = copy.deepcopy(pds)
        changed["Results"][0]["Vulnerabilities"][0][field] = value
        path.write_text(json.dumps(changed))
        assert m.verify(root, policy)["failures"], field + " unexpectedly admitted"
    path.write_text(json.dumps(pds))
    (root / "app.json").unlink()
    assert m.verify(root, policy)["failures"], "Missing image admitted"
    wrong_arch = copy.deepcopy(base)
    wrong_arch["Metadata"]["ImageConfig"]["architecture"] = "arm64"
    (root / "app.json").write_text(json.dumps(wrong_arch))
    assert m.verify(root, policy)["failures"], "Different architecture admitted"
# Cross-distribution checks remain mandatory even when the scanner feed is clean.
from native_runtime_review import verify_native_sources

native_policy = json.loads(
    (ROOT / "services/security/native-runtime-review.json").read_text()
)
with tempfile.TemporaryDirectory(prefix="zing-native-review-") as folder:
    root = Path(folder)
    for name in ["pds", "plc"]:
        scan = copy.deepcopy(base)
        scan["Results"] = [
            {
                "Class": "os-pkgs",
                "Type": "alpine",
                "Packages": [
                    {"Name": k, "Version": v}
                    for k, v in native_policy["packages"].items()
                ],
            }
        ]
        facts = {
            "imageId": "sha256:fixture",
            "posixAlignedAllocationOverflowRejected": True,
            "runtime": {
                "node": native_policy["node"],
                "glibcAbsent": True,
                "shellAndBuildToolsAbsent": True,
                "ellipticCurveCrypto": True,
                "localTLS": True,
                "sqlite": True,
            },
        }
        (root / (name + ".json")).write_text(json.dumps(scan))
        (root / (name + "-runtime.json")).write_text(json.dumps(facts))
    reviewed = verify_native_sources(root, native_policy)
    assert not reviewed["failures"]
    assert (
        len(reviewed["images"]["pds"]["upstreamResiduals"]) == 4
    ), "Clean feed erased source-level risks"
    path = root / "pds.json"
    scan = json.loads(path.read_text())
    scan["Results"][0]["Packages"].append({"Name": "gcompat", "Version": "fixture"})
    path.write_text(json.dumps(scan))
    assert verify_native_sources(root, native_policy)[
        "failures"
    ], "Compatibility shim silently admitted"
    scan["Results"][0]["Packages"].pop()
    path.write_text(json.dumps(scan))
    evidence = root / "pds-runtime.json"
    facts = json.loads(evidence.read_text())
    facts["imageId"] = "sha256:different"
    evidence.write_text(json.dumps(facts))
    assert verify_native_sources(root, native_policy)[
        "failures"
    ], "Evidence from another image admitted"
    facts["imageId"] = "sha256:fixture"
    facts["runtime"]["localTLS"] = False
    evidence.write_text(json.dumps(facts))
    assert verify_native_sources(root, native_policy)[
        "failures"
    ], "Failed TLS evidence admitted"
    evidence.unlink()
    assert verify_native_sources(root, native_policy)[
        "failures"
    ], "Missing runtime evidence admitted"

print(
    json.dumps(
        {
            "status": "ok",
            "newAdvisoryRejected": True,
            "newVersionRejected": True,
            "severityEscalationRejected": True,
            "availableFixRejected": True,
            "missingImageRejected": True,
            "otherArchitectureRequiresReview": True,
            "cleanScannerFeedRetainsSourceRisks": True,
            "glibcShimRejected": True,
            "runtimeEvidenceBoundToImage": True,
        }
    )
)
