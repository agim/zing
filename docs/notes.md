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
