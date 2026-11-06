# Team notes

Lasting facts about this app that the team and every agent should know, one
dated line each, newest last: `lidza note add "..."` (MCP `lidza_note_add`)
records one; `lidza note list --grep word` (MCP `lidza_notes`) finds the ones
for an area. A convention with steps is a recipe, a choice with a reason a
decision (docs/decisions.md).
- 2026-11-05: Distribution: agim/zing remains public and open source. The owner instance is private, on zing.albaspot.com through lidza-deploy.albaspot.com. Other installers choose host/domain/storage/provider credentials in first-run settings saved encrypted in PostgreSQL. Test at completed module/release boundaries.

- 2026-11-05: Scope correction from owner: Zing must be a social server (Mastodon/Bluesky or another open-source social server), not just a client. Existing local implementation is a client foundation and does not meet server acceptance. Hosting identities, posts and server-side interoperability is required. Backend implementation choice is pending; do not treat external-account connection as hosted identity.

- 2026-11-05: Owner selected established ActivityPub and Bluesky PDS servers behind Zing. GoToSocial 0.22.3 and reference PDS 0.5.51 are pinned; local PLC identities are isolated from plc.directory. Hosted identity lifecycle is distinct from connecting/disconnecting external accounts.

- 2026-11-05: Owner authorized local Zing chat for hosted AT Protocol identities, clearly labeled separately from Bluesky-native DMs; connected Bluesky accounts retain native DM support. Owner also approved replacing the malformed PNG ownership-test fixture with a valid PNG while retaining every assertion. Rust should be used where appropriate.

- 2026-11-05: User approved separate local Zing chat for hosted AT Protocol identities; native connected Bluesky DMs remain separate. User also approved replacing the malformed upload PNG fixture with a valid PNG while retaining all assertions. Rust is used for contained image parsing, while Go/Līdza owns platform behavior.
- Local moderation uses native GoToSocial/PDS controls; the private agent must remain unreachable from public ingress. The supervisor must acknowledge GoToSocial restarts before reversible account state changes count as confirmed. No deployment has been performed.

- 2026-11-06: Owner requested completing container validation, backup/restore and dependency review locally before deployment. A single new Zing signup must enroll the user automatically on every configured hosted network. Email verification precedes native provisioning; ActivityPub and AT Protocol accounts are independent durable jobs, with partial progress/retry visible in Settings. Threads is not a self-hosted registration service and must never appear provisioned by this enrollment.

- 2026-11-06: Owner explicitly approved correcting the PLC security test to resolve Zod from its declaring @did-plc/lib dependency while retaining every assertion, then completing two-network container validation/recovery and security review locally. No deployment is authorized.

- 2026-11-06: Līdza clones the browser test database into Go package fixtures. A verified browser registration must delete itself through the auth deletion lifecycle after its assertions; otherwise a later backend mock correctly receives automatic provisioning for an unrelated leftover signup. The browser test now retains every assertion and additionally asserts fixture deletion. Cleanup applies only to its generated test account; no application behavior is bypassed.

- 2026-11-06: Owner requested safe remaining PDS/PLC fixes locally, with no deployment. The Node runtime is now musl/Node 24.22.2 with matching native SQLite, and pnpm 10.34.5 is build-only. Four upstream GNU C++ template/library cases remain tracked outside the scanner feed; a clean distribution scan must not hide them. Existing glibc-created identities/repos must pass the optional baseline-image upgrade journey before replacing runtime images.

- 2026-11-06: Owner requested fixing the four remaining C++ cases locally and considering Rust only without breaking PDS/PLC. All four upstream GCC 15 header backports now reproduce/fix their sanitizer probes and are incorporated through a checked Node 24.22.2 source rebuild plus fresh native SQLite compilation. Exact-image TLS/crypto/SQLite/hash/tamper checks, six image scans and fresh, previous-musl-image and legacy-glibc-image upgrade/recovery journeys passed. The four vendor advisory records remain visible beside local consumer remediation proof. Rust Rsky remains a separate migration assessment: its journals have restore rules that differ from Zing's current full-directory PDS backups. No deployment or runtime-image upload was performed.
