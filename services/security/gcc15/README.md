# GNU C++ header backports and builder proof

The PDS/PLC Dockerfiles use these pinned upstream header backports. This
directory also provides a standalone builder target; that builder alone is not
a release attestation. Exact service images must complete consumer rebuild,
runtime checks, image scans and the existing fresh/upgrade/recovery acceptance
journeys. The source gate retains all four vendor advisory records alongside
its separate proof of local consumer remediation.

The patches are unmodified upstream GCC 15 branch commits:

| Finding | Header | Upstream commit |
| --- | --- | --- |
| CVE-2026-102010 | GNU binary heap | [29e51a8](https://github.com/gcc-mirror/gcc/commit/29e51a820d76c766ae417fcba912714337b0eb17) |
| CVE-2026-114690 | GNU parallel search | [b346c34](https://github.com/gcc-mirror/gcc/commit/b346c341218a6a9a52c56e2bd1516b35b51c2bd2) |
| CVE-2026-114691 | Boolean vector constructors | [dfc1501](https://github.com/gcc-mirror/gcc/commit/dfc15010eda603e814caf9681a76c726dea2bf64) |
| CVE-2026-115455 | Wide bitset stream extraction | [2ffbcde](https://github.com/gcc-mirror/gcc/commit/2ffbcde8634f69ae6086e2677bd90ecbad687f9f) |

`prepare.py` checks patch hashes and compiler version, then applies only the
header hunks with zero fuzz. It does not edit upstream tests. The installed
`std/bitset` path is mapped to `bitset`. The resulting header hashes are retained
in `/gcc-header-evidence.json`. GCC patch licensing follows the original source;
the GPL and GCC Runtime Library Exception are in `../licenses/`.

`check.py` compiles four independent Zing probes with AddressSanitizer and
UndefinedBehaviorSanitizer. Each must reproduce its specific baseline defect;
after patching each must exit successfully with correct ordinary behavior and
no sanitizer diagnostic. No result is inferred from a clean package scan.

From the repository root, on a Linux Docker host:

```sh
docker build --target headers -t zing-node-headers:experiment \
  -f services/security/gcc15/Dockerfile services
docker build --target node-build -t zing-node-patched:experiment \
  -f services/security/gcc15/Dockerfile services
```

The second target rebuilds Node 24.22.2 from its SHA-256 checked release archive.
It uses one compiler worker because two simultaneous V8 compilations caused severe pressure in
the local validation lab's 3 GiB memory budget. The source build can take a long
time on that machine. An interrupted build may retain its completed objects only
when the exact source, patched headers, compiler and configure flags are kept.
It is deliberately a build image. Do not run a production service in it or
replace a validated image merely because the header probes pass. Native addons
also require compilation using the patched headers. Replacing a shared library
does not repair template code baked into existing consumers.

Both service Dockerfiles build from the same source steps by default. A local
checked builder can be reused with BuildKit, which skips the unused source
stages (the legacy builder does not):

```sh
docker buildx build --load --build-arg ZING_NODE_BUILD_IMAGE=zing-node-patched:experiment \
  -t zing-pds:release-check -f services/pds/Dockerfile services
docker buildx build --load --build-arg ZING_NODE_BUILD_IMAGE=zing-node-patched:experiment \
  -t zing-plc:release-check -f services/dev-plc/Dockerfile services
```

Use only a trusted builder produced from this recipe. Service builds still
require its exact patched headers, baseline/fixed probe results, Node source
checksum and V8 compiler evidence. Runtime collection verifies the actual Node,
libstdc++ and every native addon hash, including rejection of a falsified Node
manifest. This cache option does not waive the image scan or acceptance gate.
The builder must have no existing `/services` tree, and installation uses a new
pnpm store with native source compilation forced, preventing reuse of an old
addon from a cached application build.
