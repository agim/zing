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
- PDS/PLC: Node 24.22.2 on musl, built from the official Alpine image into a
  six-package runtime root. GNU libc and compatibility shims are absent. Native
  SQLite is compiled for musl using the matching Node headers. The scratch
  runtime contains no shell, compiler or package manager; package metadata,
  certificates, timezone data and licenses remain present. pnpm 10.34.5 is used
  only during builds. Native SQLite, TLS trust/rejection and elliptic-curve
  crypto checks pass under a non-root/read-only/capability-dropped runtime.
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

## Remaining source-level findings

The new PDS/PLC image scans report no vulnerabilities. This does **not** erase
known issues that Alpine's scanner feed does not list. The source review is
independent of the distribution scan and records four remaining GCC issues.

The old images had 37 package findings each, representing 27 distinct CVEs:
22 in GNU libc and five in the GCC source package. The 22 GNU libc issues no
longer describe shipped code: the library, loader and compatibility shims are
absent. Runtime checks verify the musl loader and lack of glibc; this is actual
component removal rather than a change in scanner labels.

CVE-2026-95619 affects aligned allocation paths which round sizes; upstream
explicitly distinguishes the POSIX allocation path used here. The build checks
normal alignment and rejection of near-SIZE_MAX requests, then requires the
runtime libstdc++ bytes to match the tested library. See the
[upstream fix and platform explanation](https://github.com/gcc-mirror/gcc/commit/638de129df3a22917ff2eda13bd0fd861ca9befc).

The following remain conservatively tracked against GCC 15.2.0-r5 and consumer
builds. An affected native HTTP request path has not been established; that is
not a claim of non-affectation.

| Finding | Component | What is still needed |
| --- | --- | --- |
| [CVE-2026-102010](https://security-tracker.debian.org/tracker/CVE-2026-102010) | GNU binary-heap priority queue erase_if | The upstream fix must reach the matching stable toolchain and affected consumer builds. |
| [CVE-2026-114690](https://security-tracker.debian.org/tracker/CVE-2026-114690) | GNU parallel-search bounds | Upstream patch exists; it is not carried by the pinned stable toolchain. |
| [CVE-2026-114691](https://security-tracker.debian.org/tracker/CVE-2026-114691) | Oversized vector<bool> construction | Compiler headers and affected consumers need the upstream correction. |
| [CVE-2026-115455](https://security-tracker.debian.org/tracker/CVE-2026-115455) | Wide-character bitset extraction | Upstream patch exists; it is not carried by the pinned stable toolchain. |

The [Alpine compiler recipe](https://github.com/alpinelinux/aports/blob/3.24-stable/main/gcc/APKBUILD)
records the stable package and patches. Replacing just a shared library cannot
establish a fix for template code already compiled into Node. Mixing unstable
libraries or making a private V8/compiler fork is not a safe maintenance update.
These four items therefore stay visible pending maintained upstream
compiler/Node builds, with reachability and source status rechecked before
production. No deployment has been performed.

The [Node 24.22.2 release](https://nodejs.org/en/blog/release/v24.22.2) includes V8
and stream backports. [pnpm 10.34.5](https://github.com/pnpm/pnpm/releases/tag/v10.34.5)
also closes build-time path traversal cases. The scoped Zod/deepmerge backports
below retain their original version alerts.

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
Also collect `pds-runtime.json` and `plc-runtime.json` with each exact ImageID
and the JSON from `services/security/node-runtime-check.mjs --tls` (plus `--pds`
for PDS), using a disposable local TLS fixture; never export its private key.

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
`services/security/native-runtime-review.json` adds a mandatory cross-distribution
source review: exact runtime inventory and matching per-image TLS/crypto/SQLite
proof are required. The four upstream risks remain in the output even with a
clean scanner feed. Tests reject glibc compatibility shims, mismatched image
proof and failed TLS checks. The earlier distribution findings remain in
`runtime-review.json` as rollback-baseline evidence; they cannot bypass the
candidate's required musl checks.

The upgrade journey starts from the previous glibc PDS/PLC images, creates real
native accounts and posts, then replaces both processes on the same volumes
before encrypted fresh-volume recovery. Native repository URIs/CIDs are checked
directly against PDS, beyond the app's read model. Linux amd64 is the validated
platform; other architectures need their own review.
