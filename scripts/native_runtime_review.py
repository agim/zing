"""Cross-distribution source review; a clean scanner feed cannot erase known issues."""

import json


def verify_native_sources(folder, policy):
    result = {
        "status": "reviewed_with_upstream_residuals",
        "images": {},
        "failures": [],
    }
    for name in ["pds", "plc"]:
        try:
            scan = json.loads((folder / (name + ".json")).read_text())
            facts = json.loads((folder / (name + "-runtime.json")).read_text())
        except (OSError, ValueError):
            result["failures"].append(name + ": missing runtime evidence")
            continue
        os_results = [r for r in scan.get("Results", []) if r.get("Class") == "os-pkgs"]
        packages = {
            p["Name"]: p["Version"] for r in os_results for p in r.get("Packages", [])
        }
        if not os_results or any(r.get("Type") != "alpine" for r in os_results):
            result["failures"].append(name + ": musl distribution inventory required")
        if any(packages.get(k) != v for k, v in policy["packages"].items()):
            result["failures"].append(
                name + ": runtime package version requires review"
            )
        if any(k in packages for k in ["libc6", "glibc", "gcompat", "libc6-compat"]):
            result["failures"].append(
                name + ": GNU libc or compatibility shim reintroduced"
            )
        runtime = facts.get("runtime", {})
        expected = [
            "glibcAbsent",
            "shellAndBuildToolsAbsent",
            "ellipticCurveCrypto",
            "localTLS",
        ] + (["sqlite"] if name == "pds" else [])
        if (
            facts.get("imageId") != scan.get("Metadata", {}).get("ImageID")
            or runtime.get("node") != policy["node"]
            or any(runtime.get(k) is not True for k in expected)
        ):
            result["failures"].append(
                name + ": matching runtime/TLS/native evidence required"
            )
        if (
            name == "pds"
            and facts.get("posixAlignedAllocationOverflowRejected") is not True
        ):
            result["failures"].append(
                name + ": aligned allocation regression evidence required"
            )
        result["images"][name] = {
            "imageId": facts.get("imageId"),
            "packages": {k: packages.get(k) for k in policy["packages"]},
            "gnuLibc": "absent",
            "upstreamResiduals": policy["upstreamResiduals"],
            "alignedAllocation": policy["alignedAllocation"],
        }
    if result["failures"]:
        result["status"] = "failed"
    return result
