"""Apply pinned upstream GCC-15 header hunks, without fuzz or test changes."""

import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent
PATCHES = {
    "heap": "7b703b0b9b5af62245571883390bbf82d3d8c717c600aa611f1721751f3079d0",
    "search": "03283e7d62894a9edd8cf4b92f7ac758bc8b263946b08f5bd14887bb269ce31b",
    "vector": "1629f2f44cf6364dd46881d5a2d2323e3f182ce62fafca1fa468a236eea13ab3",
    "bitset": "e2e48abd551fa659174294002a0c7387a51c09d5dc90b382d3819cafed2b7f2f",
}
HEADERS = {
    "heap": "ext/pb_ds/detail/binary_heap_/erase_fn_imps.hpp",
    "search": "parallel/search.h",
    "vector": "bits/stl_bvector.h",
    "bitset": "bitset",
}


def main():
    version = subprocess.check_output(["g++", "-dumpfullversion"], text=True).strip()
    if version != "15.2.0":
        raise SystemExit("Backports require reviewed GCC 15.2.0")
    include = Path("/usr/include/c++") / version
    evidence = {}
    for name, sha in PATCHES.items():
        raw = (ROOT / "patches" / (name + ".patch")).read_bytes()
        if hashlib.sha256(raw).hexdigest() != sha:
            raise SystemExit("Upstream patch hash mismatch: " + name)
        chunks = raw.decode().split("diff --git ")[1:]
        header = next(c for c in chunks if c.startswith("a/libstdc++-v3/include/"))
        # Upstream's include/std/bitset installs as include/c++/VERSION/bitset.
        if name == "bitset":
            header = header.replace("include/std/bitset", "include/bitset")
        subprocess.run(
            ["patch", "--batch", "--fuzz=0", "-p3", "-d", str(include)],
            input="diff --git " + header,
            text=True,
            check=True,
        )
        evidence[name] = {
            "patchSha256": sha,
            "headerSha256": hashlib.sha256((include / HEADERS[name]).read_bytes()).hexdigest(),
        }
    Path("/gcc-header-evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")


if __name__ == "__main__":
    main()
