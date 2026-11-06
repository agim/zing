# Decisions

What shaped zing and why, one entry each, newest last: a pack added, Rust
chosen for a module, a dependency taken, a schema tradeoff, an
integration. Read this before working in those areas. Record yours in the
same commit as the change: `lidza decision add "Title" --why "..."`
(MCP: `lidza_decision_add`). A recipe in docs/lidza-guide.md says how
this app does something; a decision here says why it is done that way.

## 2026-11-05: Pack auth added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack jobs added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack mail added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack cache added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack realtime added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack storage added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Who owns the data: Each user exclusively owns their connections, credentials, drafts, me…

Why: Answered in the brief interview (docs/brief.md): Each user exclusively owns their connections, credentials, drafts, media, schedules and deliveries. Realtime topics require the same owner. Every query is scoped by it; the most expensive answer to change later.

Touches: docs/brief.md

## 2026-11-05: Sign-in: Independent email/password through Lidza auth.Mount, email verificati…

Why: Answered in the brief interview (docs/brief.md): Independent email/password through Lidza auth.Mount, email verification, reset, sessions and account deletion; social connection is separate. Configures auth.Mount and the sign-in page.

Touches: docs/brief.md

## 2026-11-05: Palette: Use supplied Zing SVGs and electric blue #0085F3, black and white; fo…

Why: Answered in the brief interview (docs/brief.md): Use supplied Zing SVGs and electric blue #0085F3, black and white; follow device theme with manual override. Written into the design tokens (src/index.css) and the admin theme; agents never fall back to a stock blue.

Touches: docs/brief.md

## 2026-11-05: Pack db added

Why: PostgreSQL owns user data, durable delivery states, transactional job enqueue and per-account isolation.

## 2026-11-05: Admin page First-run setup

Why: Only a claimed/authorized operator configures instance-wide origin, private/public access, mail, storage and optional Threads credentials. Values are sealed in PostgreSQL through the framework store. The page summarizes no users social data and never reveals saved secrets.

## 2026-11-05: Established social servers behind Zing

Why: The owner selected established ActivityPub and Bluesky PDS servers rather than a native Go protocol implementation. GoToSocial 0.22.3 supplies ActivityPub, signatures, relationships, timelines, moderation and deliveries. The reference PDS 0.5.51 supplies identity, repository signing, blobs, authentication and synchronization. Its dependency lock is retained from the official PDS service, avoiding incompatible transitive versions encountered with an unconstrained npm install. Zing remains a separate Go/Līdza application; upstream code is not patched.

Local development uses the upstream PLC package with its own PostgreSQL database and loopback-only listeners. Production uses the public PLC directory. Local PDS views read actual repository records and are labeled as local; optional AppView reads and native chat are separate capabilities. Provisioning reservations and generated passwords are persisted before remote creation, tokens are sealed, and retries reconcile the same identity. A backend-confirmed unavailable PDS name can be changed; ambiguous creation cannot silently create a replacement identity.

Touches: internal/hosting, internal/providers, services, scripts/social-local.py, deploy/compose.social.yaml, schema.lidza

## 2026-11-05: Uploaded images in a Rust/WASM pack

Why: User-supplied image parsing must be contained. The media_guard pack uses upstream Rust image codecs under a 256 MiB memory limit and five-second deadline, with no filesystem or network access. It fully decodes bounded still images, rejects animated/damaged/oversized payloads, resizes to 1400 pixels and emits canonical PNG without original metadata. Go retains authentication, ownership, storage and HTTP behavior. The owner approved fixing the existing header-only PNG test fixture, retaining every ownership assertion.

Touches: packs/media_guard, handlers/media.go, schema.lidza, tests/social_test.go

## 2026-11-05: Dependency github.com/bluesky-social/indigo

Why: Official AT Protocol OAuth, identity and record-key parsing replace handwritten protocol validation. The pinned dependency supplies PKCE/PAR/DPoP and canonical TID/URI/CID/DID syntax for the hosted read model and idempotent writes.

## 2026-11-05: Compatible service dependency security backports

Why: The owner requested completion of local security work. The PDS is upgraded to 0.5.52, compatible current dependencies are pinned, and the old bundled PLC distribution is replaced by maintained unbundled upstream source at commit 29f1a70ee9f6f80a25946cac103b2f745e371334. Original source and MIT license remain inside the vendor archives. Patched Kysely 0.28.19 preserves the PLC migration API. A scoped Zod 3 error-retention backport preserves PDS OAuth compatibility, and deepmerge rejects prototype-control keys. These last two packages still produce version-only audit alerts; the fixes are recorded as verified backports, never hidden by a scanner exclusion or a false upstream version. Isolated database snapshots passed actual account/auth/repository/SMTP/deletion compatibility checks before the running services were changed.

Touches: services/pds, services/dev-plc, services/vendor, scripts/social-local.py

## 2026-11-05: Local community index and consented Zing chat

Why: The owner approved a clearly separate private Zing chat service for hosted AT Protocol identities, preserving native DMs for connected accounts. Public PDS records belong in a restartable PostgreSQL read model with source URI/CID, not an in-memory roster scan. Durable source-page checkpoints, mutual blocks and moderation filters govern the local community view. Private messages remain sealed in Zing, require recipient acceptance, and never enter public repositories or relay jobs. Realtime is an invalidation hint with an owner-only subscription; cached content remains visible during reconnects.

Touches: internal/atview, internal/chat, db/queries, schema.lidza, src/live.ts, src/Zing.tsx

## 2026-11-05: Audited native moderation with deletion independent of suspension

Why: Moderation must change the established servers, not simulate application flags. Operator pages use the framework admin authorization and verified accounts. Local suspension takes effect immediately; native backend confirmation and per-network checkpoints follow through durable jobs. Restoration clears the local guard only after both servers confirm. GoToSocial reversible disable requires a supervisor-confirmed restart; destructive native suspension is reserved for a user-requested erasure whose disabled session cannot self-delete. Reports expose only public selected content or the single private message a participant explicitly selected. Deletion and synchronization/chat/moderation share advisory locks to prevent orphaned or recreated private records.

Touches: internal/moderation, internal/hosting/deletion.go, app/admin/moderation.html, services/activitypub, scripts/social-local.py

## 2026-11-05: Stable GoToSocial tombstone email minimization

Why: Actual suspended-account erasure acceptance exposed stable 0.22.3 retaining email addresses in native user tombstones. The private agent’s SQLite supplement verifies the native actor’s final suspension origin/timestamp, which native Delete writes after its cleanup, before replacing retained email/unconfirmed email with an anonymous operation alias. It restarts to evict cached user data and removes provisioning email from the operation journal. This preserves required immutable protocol tombstones and username reuse protection. New ActivityPub provisioning uses service aliases instead of copying real Zing emails; existing operation journals retain their original identity during upgrades/reconciliation. The regression verifies authentication, operation binding, refusal to alter active/non-final actors and idempotency. External/custom PostgreSQL backends must supply an equivalent reviewed agent; the reference deployment explicitly uses SQLite.

Touches: services/activitypub/agent.py, internal/hosting, internal/moderation/erasure.go, scripts/verify-agent-security.py

## 2026-11-05: Admin page Moderation

Why: Verified operators need cross-account reports, selected sealed evidence, hosted-user suspension/restoration and native domain/post policy controls. The framework’s existing admin authorization and CSRF protection guard every callback; business callbacks also require verification and an unsuspended operator. Ordinary social users can only submit their own reports/appeals. The page reads the bounded moderation report/action queues and local auth user directory; it has no general private conversation reader.

Touches: internal/moderation/admin.go, app/admin/moderation.html

The generated request-key migration adds a volatile UUID default only to the new moderation action table, introduced empty in the same unreleased module. Initial deployment applies it before accepting traffic. Existing installations of this development module should apply it in a maintenance window; the L021 migration warning is retained for review rather than editing generated SQL.

## 2026-11-06: One verified signup enrolls every configured hosted network

Why: The owner explicitly requested one Zing signup to provide hosted Mastodon-compatible ActivityPub and Bluesky/AT Protocol accounts. Enrollment is persisted in the auth transaction, with a stable generated native username (two ASCII name characters plus 64 random bits, within PDS service-handle limits) and independently unique jobs per owner/network. Native creation waits for verified email and current private-instance authorization. A durable sweep recovers missed events and newly configured networks; exhausted retries require an owner action. Existing pre-enrollment users retain their identities and manual creation path. The already-active network is preserved across retries, restarts and failure of another server. Unsupported commercial services are not represented as provisioned accounts.

Touches: internal/hosting/enrollment.go, schema.lidza, db/queries/enrollment.sql, app/routes.go, src/Zing.tsx

## 2026-11-06: Encrypted quiescent snapshots with isolated restore validation

Why: Database-only backups cannot preserve hosted identities. A consistent recovery boundary includes application and PLC PostgreSQL, native SQLite/repositories/blobs, sealed configuration/master key and application objects. The operator stops all writers first; logical PostgreSQL tools must match the server major. Snapshots use independent age recipients and SHA-256 manifests. Restore requires a trusted inventory hash and a new destination, rejects links/traversal and verifies all files before publishing the extracted directory. Recovery drills use fresh databases/volumes and disable outbound workers while checking native DID/repository continuity and private credential decryption. Recovery identities are never stored in snapshots or source control. S3 installations need matching object-storage snapshots/versioning rather than assuming local files cover remote objects.

Touches: scripts/backup-instance.py, scripts/verify-recovery-local.py, scripts/verify-containers-local.py, cmd/recovery-check

## 2026-11-06: Keep compatible Zod 3 with leading bounds and bounded errors

Why: PDS OAuth uses the Zod 3 refinement API. A version substitution with Zod 4 breaks that API. Reviewing upstream's collection-bound fix showed an error-count cap alone did not prevent expensive element traversal. The scoped v3 backport rejects oversized array max/exact-length and set max constraints before element work, in both CJS/ESM builds, while retaining the 64-error bound. Clean-install PLC checks and full two-network container/recovery acceptance pass with the owner-approved lookup correction and all original assertions retained. Regression checks cover async parsing, nested paths, valid input and refinement compatibility. Version-based advisories remain visible and are accepted only for the checksum-pinned, regression-tested patch.

Touches: services/pds/patches, services/dev-plc/patches, services/security/zod-check.mjs


## 2026-11-06: Scan the complete runtime and keep residual findings explicit

Why: Application-only audits missed build tools, old privilege-drop helpers and the native ActivityPub executable. Stable GoToSocial source is rebuilt with compatible pinned security dependencies and an AGPL source offer; PostgreSQL keeps its official server/entrypoint while gosu is rebuilt with patched Go/modules. Node service images contain the actual runtime libraries and their package metadata rather than package-manager/build caches or unused system zlib/OpenSSL/OpenMP. Full image scans now have no high/critical findings. Existing medium/low glibc/GCC findings remain visible, version-scoped and documented with primary distribution sources. Unknown/new/actionable findings fail the release review gate. Swapping scan feeds or claiming unproven non-affectation is not a fix.

Touches: Dockerfile, services/activitypub/security, services/postgres, services/pds/Dockerfile, services/dev-plc/Dockerfile, services/security/runtime-review.json, scripts/verify-image-security.py

## 2026-11-06: Validate native state with fresh disposable container restores

Why: The full acceptance run must prove native signup and data continuity without borrowing live credentials, existing volumes or public identities. Each fixture generates its own keys, requires SMTP verification, injects PDS failure/app restart, publishes real canonical media to both native servers and then quiesces writers. Recovery restores all PostgreSQL tables and native volumes into another project, verifies every SQLite store and source ID/blob/private media, and keeps outbound workers disabled. Linux loopback ingress reaches an internal Docker network without opening an outbound gateway. PostgreSQL readiness uses a real TCP query to avoid the temporary init server. Native-profile and archive-rejection drills are separate corroborating checks.

Touches: deploy/compose.acceptance.yaml, scripts/verify-containers-local.py, scripts/container_bridge.py, scripts/verify-recovery-local.py


## 2026-11-06: Remove GNU libc from native Node runtimes with upgrade proof

Why: The owner asked to fix safe remaining PDS/PLC findings. Node 24.22.2 and pnpm 10.34.5 are compatible patch updates. A musl runtime removes GNU libc's affected code without mixing unstable distribution libraries; better-sqlite3 is rebuilt for the new ABI. The minimal scratch root retains real APK metadata/licenses, certificates and timezone data while excluding shell/build tools. TLS, crypto, SQLite and non-root/read-only checks precede both a fresh-install journey and an upgrade using existing glibc-created identities/posts/volumes. A POSIX aligned-allocation regression plus runtime library byte comparison addresses the platform-specific GCC case. Four C++ source findings remain visible independently of scanner coverage; swapping distributions is not evidence that compiled templates were fixed.

Touches: services/pds/Dockerfile, services/dev-plc/Dockerfile, services/security, scripts/native_runtime_review.py, scripts/verify-containers-local.py
