# PDS/PLC C++ remediation and Rust alternatives

Review date: 2026-11-06. No deployment is authorized. The production image source
review remains the authority for which exact images have completed validation.

## Targeted backports

All four remaining findings have fixes on the upstream GCC 15 branch. They
change template headers, not a replaceable JavaScript package. The isolated
experiment in `services/security/gcc15/` pins their original patches and checks
their hashes. With GCC 15.2.0 on Alpine 3.24, all four independent probes
reproduce before patching and pass with the backports, under address/undefined
behavior sanitizers. The changes preserve the public container layouts and
avoid a compiler major upgrade. This establishes backport feasibility, not
application acceptance or general absence of memory defects.

The exact SHA-256 checked Node 24.22.2 source contains Boolean vector uses in
V8's compiler, profiler, tracing and WebAssembly code. Finding these uses does
not establish an exploitable request path, but it prevents claiming that the
whole application is unaffected just because a dynamic symbol search is empty.
A fixed toolchain must build Node/V8 and the better-sqlite3 addon; a library swap
alone cannot fix old template instantiations. Existing PDS databases, blobs,
repository URIs/CIDs and PLC identities must survive the replacement and restore
journeys before the four findings can be closed for replacement images.

The shared-library review covers GCC 15.2.0's runtime `src`, `libsupc++` and
`config` trees (456 files), plus the 45 recipe/patch files at Alpine build commit
[`423a8ad`](https://github.com/alpinelinux/aports/tree/423a8ad043d07f2c7546c8ec3e2b0384cda360ae/main/gcc),
the commit recorded by the installed APK. Source blobs were checked against
their Git IDs. No direct uses of the four affected template families were found
in that runtime source, and the tested shared library exports no affected
instantiations. This supports retaining its ABI and requiring patched consumer
builds; it is not a general reachability proof. The runtime also verifies the
shared library's actual bytes against its build evidence.

The exact local replacement images passed five selected upstream Node core
tests, both services' runtime/TLS/hash/tamper checks, the complete image review,
and fresh-install, previous-musl-runtime and legacy-glibc-runtime upgrade journeys. Each journey
included automatic verified signup on both networks, native media publishing,
restart recovery and encrypted restore into fresh volumes/databases. Native
repository URIs/CIDs and blob bytes survived, all tables in both PostgreSQL
databases matched, five SQLite stores passed integrity checks, and restored
outbound workers stayed disabled. These results are scoped to the recorded
Linux amd64 image IDs; no deployment or runtime-image publication occurred.

## Rust options

| Option | Assessment for this platform |
| --- | --- |
| [Deno](https://github.com/denoland/deno) | Its core uses Rust, but it still embeds V8. It does not eliminate the C++ engine. Its Node compatibility and native addon behavior need independent PDS/PLC validation; it is not evidence of remediation. |
| [Boa](https://github.com/boa-dev/boa) | A Rust JavaScript engine, described upstream as experimental. An engine is not the Node platform API required by these servers. Adopting it would require implementing and validating substantial runtime compatibility. |
| [rsky](https://github.com/blacksky-algorithms/rsky) | A real Rust AT Protocol server with a documented coexistence/router migration path, shared SQLite storage layout, disk/S3 blobs and SMTP. This is substantially closer than a rewrite. It still replaces the server, rather than one C++ dependency; Zing-specific compatibility and migration acceptance have not been established. |
| Replace better-sqlite3 with a Rust binding | PDS depends on the existing driver contract. A new binding needs a compatibility layer and transaction/migration/backup tests; SQLite itself is C, and Node/V8 would still be C++. This does not resolve all four compiler findings. |

The assessed safe direction is the narrow upstream header backport with rebuilt
consumers and exact-image acceptance. A Rust PDS should be treated as a separate
server migration, preserving DID keys and repository history, rather than a
security patch to the current installation. Zing's existing contained image
decoder remains the appropriate Rust/WASM boundary for untrusted media.

Rsky source review is pinned to
[`3437facef709251d5cdd51148b2355250f7c0c59`](https://github.com/blacksky-algorithms/rsky/tree/3437facef709251d5cdd51148b2355250f7c0c59),
whose PDS manifest reports 1.14.16. Its general README still contains an older
pre-1.0 warning; that warning alone is not a current version assessment. Its
PDS documentation describes reference compatibility fixtures for 0.5.27, while
Zing pins 0.5.52. The differences need an actual clone migration exercise. The
documented new lifecycle, blob-attempt and repair journals also require adapting
our backup/restore rules: some journals explicitly must never be restored from
a backup. No Rsky dependency audit or runtime acceptance has been performed, and
the proposal would not replace the separate Node PLC fixture.

This is a concrete integration gap: `scripts/backup-instance.py` copies each
configured data directory recursively, and both recovery harnesses capture the
whole PDS directory/volume. Rsky's documented default blob-attempt journal lives
under that directory, while its documentation explicitly prohibits restoring it
from backup. Its generation registry likewise must not roll back during restore.
Replacing the server without adapting those boundaries would violate Rsky's
recovery contract even if its XRPC routes worked. This review does not change
Zing's current reference-PDS backup format or attempt an unvalidated migration.
