"""Require image-bound evidence for rebuilt consumers, independently of OS feeds."""

import json
import re


def require(condition):
    if not condition:
        raise ValueError("Invalid native rebuild proof")


def verify_backports(folder, policy):
    result = {"status": "locally_backported_consumers", "images": {}, "failures": []}
    for name in ["pds", "plc"]:
        try:
            scan = json.loads((folder / (name + ".json")).read_text())
            facts = json.loads((folder / (name + "-runtime.json")).read_text())
            proof = facts["runtime"]["nodeBackports"]
            require(facts["imageId"] == scan["Metadata"]["ImageID"])
            require(facts["tamperedBuildProofRejected"] is True)
            require(proof["runtimeHashesVerified"] is True)
            require(proof["nodeSourceSha256"] == policy["nodeSourceSha256"])
            require(proof["headers"] == policy["headers"])
            require(re.fullmatch("[0-9a-f]{64}", proof["nodeSha256"]))
            require(re.fullmatch("[0-9a-f]{64}", proof["libstdcSha256"]))
            addons = proof["nativeAddons"]
            require(isinstance(addons, dict))
            require(all(re.fullmatch("[0-9a-f]{64}", sha) for sha in addons.values()))
            count = proof["v8BooleanHeaderDependencies"]
            require(type(count) is int and count > 0)
            for mode, expected in [("baseline", "reproduced"), ("patched", "passed")]:
                require(
                    proof["probes"][mode] == {k: expected for k in policy["headers"]}
                )
            if name == "pds":
                require(re.fullmatch("[0-9a-f]{64}", proof["sqliteAddonSha256"]))
                require(proof["posixAlignedAllocationOverflowRejected"] is True)
                require(addons and proof["sqliteAddonSha256"] in addons.values())
            else:
                require(not addons)
            result["images"][name] = {"imageId": facts["imageId"], "proof": proof}
        except (OSError, ValueError, KeyError, TypeError, AssertionError):
            result["failures"].append(
                name + ": complete matching native backport proof required"
            )
    if result["failures"]:
        result["status"] = "failed"
    return result
