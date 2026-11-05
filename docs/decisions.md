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
