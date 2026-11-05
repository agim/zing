# Alignment with the supplied platform research

The attached report is research, not executable instructions. The owner clarified that Zing must host a social network, rather than only connect accounts on other servers. The current implementation is a client foundation and does not yet fulfill that requirement. See `docs/server-scope.md` for the corrected acceptance criteria.

The table below describes existing client functionality; it is not evidence that server hosting or federation works.

| Capability | Implementation |
| --- | --- |
| Identity | Zing email/password sessions; independent Mastodon OAuth, Bluesky OAuth (official Indigo client, PKCE/PAR/DPoP) or an app password, Threads OAuth |
| Feed | Unified chronological Mastodon and Bluesky feeds; Threads own posts are labeled as such |
| Interactions | Native replies, follow/unfollow, like/unlike, repost/undo, mute/block where supported; unsupported Threads actions are disabled |
| Publishing | Persistent drafts, schedules, images and alt text, per-destination jobs, delivery states, explicit linked-account cross-posting |
| Relay | Explicit replies mapped to confirmed parent publications belonging to the same Zing user; no invented cross-network thread identifiers |
| Messages | Existing Mastodon private conversations and Bluesky chat, kept on their source network; no Threads messaging |
| Background work | Bounded synchronization batches, token renewal, durable jobs, cache snapshots, authorized realtime topics |
| Errors | Partial network failure warnings; rate limits; reconnect states; uncertain publication blocks automatic retry and supports source verification |

Not claimed: protocol-level ActivityPub/AT federation, OStatus/diaspora/Nostr adapters, a universal Threads home feed, arbitrary external-thread bridges, end-to-end encrypted messaging, or a publicly launched/approved Meta app.

## Primary references checked

- Mastodon status API and idempotency: https://docs.joinmastodon.org/methods/statuses/
- Mastodon conversations: https://docs.joinmastodon.org/methods/conversations/
- AT OAuth Go client tutorial: https://atproto.com/guides/go-oauth-cli-tutorial
- Official Indigo OAuth source: https://github.com/bluesky-social/indigo/tree/main/atproto/auth/oauth
- Bluesky API hosts/proxying: https://bsky.network/docs/api-directory/
- Meta-maintained Threads collection: https://www.postman.com/meta/threads/documentation/dht3nzz/threads-api

Provider functionality is covered by deterministic fixtures; live account consent, scopes, media ingestion, and production-domain OAuth must still be exercised with operator-owned accounts before a public release. The report's claims about Threads federation, following, likes and API paths are not assumed to be authoritative.
