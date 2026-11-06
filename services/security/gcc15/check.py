"""Prove baseline failures and repaired semantics using the same sanitizer flags."""

import json
import os
from pathlib import Path
import subprocess
import sys

mode = sys.argv[1]
assert mode in {"baseline", "patched"}
results = {}
for case, name in enumerate(["heap", "search", "vector", "bitset"], start=1):
    binary = "/tmp/gcc-check-" + name
    subprocess.run(
        ["g++", "-std=c++20", "-g", "-O1", "-fno-omit-frame-pointer",
         "-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fopenmp",
         "-DCASE=" + str(case), str(Path(__file__).with_name("check.cpp")), "-o", binary],
        check=True,
    )
    result = subprocess.run(
        [binary], capture_output=True, text=True, timeout=30,
        env={**os.environ, "OMP_NUM_THREADS": "2", "ASAN_OPTIONS": "detect_leaks=0"},
    )
    Path("/tmp/" + mode + "-" + name + ".log").write_text(result.stderr)
    if mode == "baseline":
        signatures = {
            "heap": "heap-use-after-free", "search": "dynamic-stack-buffer-overflow",
            "vector": "Assertion", "bitset": "insufficient space for an object of type 'wchar_t'",
        }
        assert result.returncode != 0 and signatures[name] in result.stderr, (name, result.stderr)
    else:
        assert result.returncode == 0 and not result.stderr, (name, result.stderr)
    results[name] = "reproduced" if mode == "baseline" else "passed"
Path("/gcc-" + mode + "-checks.json").write_text(json.dumps(results, indent=2) + "\n")
print(mode, results, flush=True)
