# Team notes

Lasting facts about this app that the team and every agent should know, one
dated line each, newest last: `lidza note add "..."` (MCP `lidza_note_add`)
records one; `lidza note list --grep word` (MCP `lidza_notes`) finds the ones
for an area. A convention with steps is a recipe, a choice with a reason a
decision (docs/decisions.md).
- 2026-11-05: Distribution: agim/zing remains public and open source. The owner instance is private, on zing.albaspot.com through lidza-deploy.albaspot.com. Other installers choose host/domain/storage/provider credentials in first-run settings saved encrypted in PostgreSQL. Test at completed module/release boundaries.

- 2026-11-05: Scope correction from owner: Zing must be a social server (Mastodon/Bluesky or another open-source social server), not just a client. Existing local implementation is a client foundation and does not meet server acceptance. Hosting identities, posts and server-side interoperability is required. Backend implementation choice is pending; do not treat external-account connection as hosted identity.
