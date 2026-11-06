# Container dependency review — 2026-11-06

This review includes the complete runtime filesystem, not only the application
lock files. Raw Trivy 0.76.2 results are retained privately under
`.lidza/container-lab/`; no scanner exclusions or inflated upstream versions are
used. The installed Node backports also have separate regression checks because
version-only advisories cannot detect a local patch.

## Changes validated locally

- App: x/net 0.62 and the x/crypto security commit pinned in go.mod; use maintained
  distroless Debian 13 rather than deprecated Debian 12 tags. Build Node from an
  official pinned image instead of a remote distribution-installer script.
- GoToSocial: stable 0.22.3 source with a checksum-pinned archive and updated
  compatible Go dependency graph. Corresponding source/build/license information
  ships in the image. Its Python supervisor uses a current minimal Alpine runtime
  without unused pip. The rebuilt image's scan reports no vulnerabilities.
- PDS: production dependencies only; build/package-manager caches stay out of the
  runtime. Node and its actual C++/GCC shared-library dependencies run on the
  maintained distroless base-nossl image. Package metadata and copyright files
  remain present. Unused system zlib/OpenSSL/OpenMP libraries are excluded rather
  than hiding their advisories. Read-only/capability-dropped runtime passes its
  actual backport checks and native SQLite load/query.
- PostgreSQL: official PostgreSQL 17.11 Alpine server and entrypoint; rebuild its
  unchanged gosu 1.19 helper using Go 1.27 and patched moby/sys/user and x/sys.
  Full corresponding helper source/license is retained in the image. Its new
  scan reports no vulnerabilities; the stock helper had critical/high findings.
- Valkey: maintained 8 Alpine image; its scan reports no vulnerabilities.

These are scan results for particular local image IDs, not guarantees about
future mutable tags. Rebuild, record digests, rescan and repeat acceptance before
an actual deployment. The owner approved the PLC lookup correction with every assertion retained.
The full two-network container journey passed, including a failed PDS/restart,
native image publishing, encrypted restore into fresh volumes, every table in
both databases, five native SQLite integrity checks, preserved IDs/blob bytes
and private media access, with restored outbound workers disabled.

## Residual PDS runtime findings

Current minimal PDS and local-fixture PLC images: each has 29 medium and 8 low
version-based Debian findings;
zero high/critical findings. Debian reports no fixed package version for these
entries. They remain in the raw report and are not described as patched.

The remaining packages are libc6, gcc-14-base, libgcc-s1 and libstdc++6. Their
source-package association can produce multiple rows for one issue. Review is
specific to the tested Linux amd64 image:

- The affected glibc monetary-formatting, tree deletion, deprecated DNS-printing,
  word expansion, gettext, iconv, glob and POSIX-regex functions are not directly
  imported by the inspected Node executable or native SQLite addon. Node uses
  its own JS regex/ICU conversion facilities. This narrows the observed surface;
  absence of a direct import does not prove absence of an indirect/dynamic call.
- nscd is not installed; the loader privilege-escalation cases require setuid/
  setgid execution, while these services run as fixed non-root users with
  no-new-privileges and dropped capabilities. Resolver configuration, LANGUAGE
  and other process environment values belong to the operator, not HTTP users.
- The strncasecmp advisory describes the Power8 optimization; the inspected
  runtime is amd64. This conclusion must be revisited for another architecture.
- The GCC-family alerts concern particular C++ priority-queue/parallel-search,
  boolean-vector, wide-bitset and aligned-allocation paths. Their affected
  template/allocation conditions have not been established as reachable through
  a native HTTP request. These are reviewed residual memory-safety/DoS findings, not
  proof of non-affectation. Native body limits, bounded Zod traversal and runtime
  isolation reduce exposure but do not replace upstream fixes.
- The low glibc mitigation-bypass/ASLR entries require an additional memory
  corruption condition or operator execution of an attacker-provided ELF; the
  service never accepts binaries for execution. They remain visible.

Upstream issue status is available from the distribution's primary tracker, for
example [glibc findings](https://security-tracker.debian.org/tracker/source-package/glibc)
and [GCC findings](https://security-tracker.debian.org/tracker/source-package/gcc-14).
The distribution marks the reviewed GCC cases as minor/no-dsa in stable; some
have fixes only in newer development packages, while others remain unfixed.
See, for example, [aligned allocation](https://security-tracker.debian.org/tracker/CVE-2026-95619)
and [parallel search](https://security-tracker.debian.org/tracker/CVE-2026-114690).
A change of libc/compiler distribution merely to obtain a different scan feed
would not establish that these paths were fixed. Recheck fixes and reachability
before production. Local acceptance is complete with these explicit residuals;
this review does not claim a vulnerability-free Node runtime.

## Compatible Node backports

Zod 3.25.76 is retained for the PDS OAuth refinement API. The patch caps retained
issues at 64 and rejects oversized array max/exact-length and set max constraints
before any element decoding in CJS and ESM, including async parsing. The deepmerge
4.3.1 patch rejects prototype-control keys. Their exact hashes are checked by
`scripts/verify-service-security.py`; unknown audit alerts fail it. Both packages
still honestly report their original version. The PLC harness now resolves Zod from its declaring @did-plc/lib dependency;
the owner approved this correction explicitly and every original assertion is
retained. Its CJS/ESM, async, migration and import checks pass in a clean container
as well as the native installation.


## Repeating the image gate

Scan all six exact built images with Trivy 0.76.2 using `--scanners vuln --format
json`, retaining complete reports named `app.json`, `activitypub.json`, `pds.json`,
`plc.json`, `postgres.json` and `valkey.json` in a private report directory. The PLC
image is the isolated local fixture, not a production public identity directory.
Record each ImageID/digest and the scanner database date with release evidence.

```sh
python3 scripts/verify-image-security.py --reports /private/image-reports \
  --out /private/image-review.json
python3 scripts/verify-image-security-checks.py
python3 scripts/verify-service-security.py
```

`services/security/runtime-review.json` records each existing residual with exact
package/version, platform, reason and primary distribution source. The gate
preserves every finding in its output. New IDs or package versions, available
fixed versions, increased severity, missing images and unreviewed architectures
fail; high/critical/unknown findings always fail. Negative regression checks
exercise those failures. This is a reviewed-residual policy, not a scanner ignore
file, fabricated version upgrade or claim that affected code has been patched.
Linux amd64 is the validated platform; other architectures need their own review.
