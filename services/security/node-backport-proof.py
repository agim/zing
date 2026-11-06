"""Bind the rebuilt Node and optional native SQLite addon to header evidence."""

import hashlib
import json
import os
import shlex
from pathlib import Path
import subprocess
import sys


def require(condition, message):
    if not condition:
        raise SystemExit(message)


security = Path(__file__).resolve().parent
policy = json.loads((security / "node-backports-policy.json").read_text())
require(
    os.environ.get("ZING_NODE_SOURCE_SHA256") == policy["nodeSourceSha256"],
    "Unreviewed Node source checksum",
)
headers = json.loads(Path("/gcc-header-evidence.json").read_text())
require(headers == policy["headers"], "Unexpected backport/header bytes")
probes = {}
for mode, status in [("baseline", "reproduced"), ("patched", "passed")]:
    probes[mode] = json.loads(Path("/gcc-" + mode + "-checks.json").read_text())
    require(probes[mode] == {k: status for k in headers}, "Missing regression evidence")

# The archive was unpacked into a fresh source directory before configure/make.
# Replay preprocessing for an actually compiled V8 Boolean-vector consumer.
# GYP's original -MMD intentionally omits system headers, so an empty .d search
# would not establish that the headers were unused.
deps = Path("/tmp/node-v24.22.2/out/Release/.deps")
commands = list(deps.rglob("heap-snapshot-generator.o.d")) or list(
    deps.rglob("traced-value.o.d")
)
require(commands, "Missing rebuilt V8 command evidence")
command = shlex.split(commands[0].read_text().splitlines()[0].split(" := ", 1)[1])
args = []
index = 0
while index < len(command):
    arg = command[index]
    if arg in {"-o", "-MF", "-MT", "-MQ"}:
        index += 2
        continue
    if arg not in {"-c", "-MMD", "-MP"}:
        args.append(arg)
    index += 1
subprocess.run(
    args + ["-E", "-o", "/dev/null", "-MD", "-MF", "/tmp/v8-header-proof.d"],
    cwd="/tmp/node-v24.22.2/out",
    check=True,
)
require(
    "/usr/include/c++/15.2.0/bits/stl_bvector.h"
    in Path("/tmp/v8-header-proof.d").read_text(),
    "V8 did not use the patched Boolean-vector header",
)
proof = {
    **policy,
    "probes": probes,
    "v8BooleanHeaderDependencies": len(commands),
    "nodeSha256": hashlib.sha256(Path("/usr/local/bin/node").read_bytes()).hexdigest(),
    "libstdcSha256": hashlib.sha256(
        Path("/runtime/usr/lib/libstdc++.so.6").read_bytes()
    ).hexdigest(),
}
if "--pds" in sys.argv:
    subprocess.run(["/tmp/aligned-allocation-check"], check=True)
    proof["posixAlignedAllocationOverflowRejected"] = True
    addon = subprocess.check_output(
        [
            "node",
            "--input-type=module",
            "-e",
            """
import {createRequire} from 'node:module';
import path from 'node:path';
const require=createRequire(process.cwd()+'/package.json');
const pds=createRequire(require.resolve('@atproto/pds'));
console.log(path.join(path.dirname(pds.resolve('better-sqlite3/package.json')),
 'build/Release/better_sqlite3.node'));
""",
        ],
        text=True,
    ).strip()
    proof["sqliteAddonSha256"] = hashlib.sha256(Path(addon).read_bytes()).hexdigest()
    addon_root = Path(addon).resolve().parents[2]
addons = sorted({p.resolve() for p in Path("/services").rglob("*.node")})
if "--pds" in sys.argv:
    require(
        addons
        and all(
            p.is_relative_to(addon_root)
            and p.name in {"better_sqlite3.node", "test_extension.node"}
            for p in addons
        ),
        "Unreviewed native addon; a fresh rebuild is required",
    )
else:
    require(not addons, "PLC acquired an unreviewed native addon")
proof["nativeAddons"] = {
    str(p.relative_to("/services")): hashlib.sha256(p.read_bytes()).hexdigest()
    for p in addons
}
destination = Path("/runtime/usr/share/zing")
destination.mkdir(parents=True, exist_ok=True)
(destination / "node-backports.json").write_text(json.dumps(proof, indent=2) + "\n")
