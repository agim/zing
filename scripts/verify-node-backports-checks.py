#!/usr/bin/env python3
"""The release gate must reject incomplete or mismatched consumer rebuild proof."""

import copy
import json
import subprocess
import sys
from pathlib import Path
import tempfile

from node_backport_review import verify_backports

ROOT = Path(__file__).resolve().parent.parent
policy = json.loads((ROOT / "services/security/node-backports-policy.json").read_text())
proof = {
    **policy,
    "runtimeHashesVerified": True,
    "nodeSha256": "a" * 64,
    "libstdcSha256": "c" * 64,
    "sqliteAddonSha256": "b" * 64,
    "nativeAddons": {"fixture/better_sqlite3.node": "b" * 64},
    "v8BooleanHeaderDependencies": 10,
    "posixAlignedAllocationOverflowRejected": True,
    "probes": {
        mode: {k: status for k in policy["headers"]}
        for mode, status in [("baseline", "reproduced"), ("patched", "passed")]
    },
}
with tempfile.TemporaryDirectory(prefix="zing-backport-check-") as temporary:
    folder = Path(temporary)
    for name in ["pds", "plc"]:
        (folder / (name + ".json")).write_text(
            json.dumps({"Metadata": {"ImageID": name}})
        )
        native_proof = copy.deepcopy(proof)
        if name == "plc":
            native_proof["nativeAddons"] = {}
        (folder / (name + "-runtime.json")).write_text(
            json.dumps(
                {
                    "imageId": name,
                    "tamperedBuildProofRejected": True,
                    "runtime": {"nodeBackports": native_proof},
                }
            )
        )
    assert not verify_backports(folder, policy)["failures"]
    original = {
        "imageId": "pds",
        "runtime": {"nodeBackports": proof},
        "tamperedBuildProofRejected": True,
    }
    cases = [
        ("nodeSourceSha256", "unreviewed"),
        ("headers", {}),
        ("nodeSha256", ""),
        ("libstdcSha256", ""),
        ("sqliteAddonSha256", ""),
        ("nativeAddons", {}),
        ("nativeAddons", {"fixture/better_sqlite3.node": "unreviewed"}),
        ("v8BooleanHeaderDependencies", 0),
        ("v8BooleanHeaderDependencies", True),
        ("runtimeHashesVerified", False),
        ("posixAlignedAllocationOverflowRejected", False),
        ("probes", {"baseline": proof["probes"]["baseline"]}),
        (
            "probes",
            {"baseline": proof["probes"]["baseline"], "patched": {"heap": "passed"}},
        ),
    ]
    path = folder / "pds-runtime.json"
    for field, value in cases:
        changed = copy.deepcopy(original)
        changed["runtime"]["nodeBackports"][field] = value
        path.write_text(json.dumps(changed))
        assert verify_backports(folder, policy)["failures"], "Accepted invalid " + field
    changed = copy.deepcopy(original)
    changed["tamperedBuildProofRejected"] = False
    path.write_text(json.dumps(changed))
    assert verify_backports(folder, policy)[
        "failures"
    ], "Accepted missing tamper rejection"
    changed = copy.deepcopy(original)
    changed["imageId"] = "another-image"
    path.write_text(json.dumps(changed))
    assert verify_backports(folder, policy)[
        "failures"
    ], "Accepted evidence from another image"
    changed = copy.deepcopy(original)
    del changed["runtime"]["nodeBackports"]
    path.write_text(json.dumps(changed))
    assert verify_backports(folder, policy)[
        "failures"
    ], "Accepted a prebuilt binary without rebuild proof"
    path.unlink()
    assert verify_backports(folder, policy)[
        "failures"
    ], "Accepted missing runtime evidence"
    # Optimization must not disable the release gate's mandatory validation.
    subprocess.run(
        [
            sys.executable,
            "-O",
            "-c",
            "import json,sys; from pathlib import Path; "
            "from node_backport_review import verify_backports; "
            "policy=json.loads(Path(sys.argv[2]).read_text()); "
            "sys.exit(0 if verify_backports(Path(sys.argv[1]),policy)['failures'] else 1)",
            str(folder),
            str(ROOT / "services/security/node-backports-policy.json"),
        ],
        cwd=ROOT / "scripts",
        check=True,
    )
print(
    json.dumps(
        {
            "status": "ok",
            "rebuildEvidenceRequired": True,
            "negativeCases": len(cases) + 4,
            "optimizedPythonCannotBypassGate": True,
        }
    )
)
