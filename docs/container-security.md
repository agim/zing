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
- PDS/PLC: Node 24.22.2 on musl, rebuilt from its checked release source with
  the four upstream GCC 15 header backports, into a six-package runtime root. GNU libc and compatibility shims are absent. Native
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

## Source findings and local consumer backports

The new PDS/PLC image scans report no vulnerabilities. This does **not** erase
known issues that Alpine's scanner feed does not list. The source review is
independent of the distribution scan and retains four GCC advisory records
alongside mandatory evidence for locally backported consumers.

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

The four vendor records remain tracked against GCC 15.2.0-r5. The service
builds apply the original upstream GCC 15 branch corrections, then rebuild
Node/V8 and native SQLite. An affected HTTP path was not established in the old
binary; absence of such a finding was never treated as remediation.

| Finding | Component | Local correction |
| --- | --- | --- |
| [CVE-2026-102010](https://security-tracker.debian.org/tracker/CVE-2026-102010) | GNU binary-heap priority queue erase_if | Retain the new allocation after shrinking. |
| [CVE-2026-114690](https://security-tracker.debian.org/tracker/CVE-2026-114690) | GNU parallel-search bounds | Allocate the additional border entry. |
| [CVE-2026-114691](https://security-tracker.debian.org/tracker/CVE-2026-114691) | Oversized vector<bool> construction | Reject invalid counts before word-count arithmetic. |
| [CVE-2026-115455](https://security-tracker.debian.org/tracker/CVE-2026-115455) | Wide-character bitset extraction | Use correctly sized/aligned character storage. |

All four independent probes reproduce with the original toolchain and pass with
the backports under address/undefined-behavior sanitizers. Patch hashes, exact
resulting header hashes and zero-fuzz application are checked. The source archive
is SHA-256 pinned. The build replays preprocessing from an actual V8 compiler
command to verify its Boolean-vector header input. Its original dependency file
uses `-MMD`, which deliberately omits system headers; an empty search there would
have been misleading. Every native addon is inventoried and bound by hash; PLC
must not acquire an unreviewed native addon.

Runtime collection independently verifies the actual Node, shared library and
native addon bytes, TLS trust/rejection, crypto and SQLite under non-root,
read-only, no-egress conditions. It also requires a falsified Node manifest to
fail with the expected hash assertion. The release gate rejects missing or
mismatched evidence even with a clean OS scan, and Python optimization cannot
turn its checks off. Existing image-policy assertions remain intact.

The [exact Alpine build recipe](https://github.com/alpinelinux/aports/blob/423a8ad043d07f2c7546c8ec3e2b0384cda360ae/main/gcc/APKBUILD)
still lacks these patches. Vendor package/Node release propagation remains
upstream work; local source builds bridge that gap without inflating versions or
silencing advisories. A library swap alone would not fix existing compiled
templates. See [the experiment and repeatable build](../services/security/gcc15/README.md)
and [the Rust alternatives/source review](cpp-rust-review.md). No deployment has
been performed.

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
Use `scripts/verify-node-runtime-local.py --reports /private/image-reports` to
collect exact-image runtime and tamper-rejection evidence. It generates and
deletes a disposable TLS key pair and uses no external network. Also collect `pds-runtime.json` and `plc-runtime.json` with each exact ImageID
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
proof are required. The four vendor advisory records remain in the output even with a
clean scanner feed; `localBackportReview` proves the compiled consumer fixes. Tests reject glibc compatibility shims, mismatched image
proof and failed TLS checks. The earlier distribution findings remain in
`runtime-review.json` as rollback-baseline evidence; they cannot bypass the
candidate's required musl checks.

The upgrade journey starts from the previous glibc PDS/PLC images, creates real
native accounts and posts, then replaces both processes on the same volumes
before encrypted fresh-volume recovery. Native repository URIs/CIDs are checked
directly against PDS, beyond the app's read model. Linux amd64 is the validated
platform; other architectures need their own review.

The header-backport module also repeated that legacy glibc journey and added an
upgrade from the previously validated musl images, alongside a fresh install.
All three complete two-network restore journeys passed for the rebuilt Node
consumers. Five selected upstream Node core tests and the exact-image runtime
and falsified-manifest checks passed before those journeys.
