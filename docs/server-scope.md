# Corrected social-server scope

## Owner clarification

On 2026-11-05 the owner clarified that Zing is supposed to be a Mastodon and Bluesky server, or another open-source social server, rather than just a client. This supersedes the client-only interpretation in earlier implementation work. The attached research informs protocol investigation; official protocol documentation governs implementation.

## Existing foundation

The current Go/Līdza app provides authentication, instance setup, encrypted settings, a branded React interface, external-account adapters, persistent drafts/media, and publication jobs. Hosted identity provisioning and server-backed reads/writes have now been added. Client-only tests remain insufficient evidence of federation; the isolated-server acceptance script exercises actual upstream servers.

## Required server behavior

- A user can create a social identity hosted by the installation, without first obtaining an account at another social service.
- Users can publish, reply, follow, like and read local content while external-account connections are absent.
- The installation owns durable identities, content, relationships, media and moderation records; deletion, export and backups include them.
- ActivityPub support requires real identity discovery, actor documents, inbox/outbox handling, authenticated inbound activities, signed outbound deliveries and persistent retry/deduplication behavior. Existing Mastodon OAuth calls are not federation.
- AT Protocol hosting requires an actual PDS with identity, signed repositories, blobs, authentication and synchronization. Existing Bluesky API calls do not create a hosted PDS. A PDS alone does not supply a complete independent Bluesky AppView, Relay or chat service.
- Cross-protocol translation is a separate explicit bridge with identity mappings and audience rules; supporting both protocols does not automatically bridge them.
- Operator controls cover registration, reports, user suspension, remote-server policies, abuse limits, failed deliveries and recovery.
- Instance membership privacy and protocol visibility are distinct settings. Restricting Zing sign-in does not make federated public content private.

## Architecture decision

The owner selected established servers. Zing now integrates GoToSocial 0.22.3 and the reference @atproto/pds 0.5.51, retaining the Go/Līdza application as the interface and account control plane. The local PDS uses an isolated upstream PLC directory backed by PostgreSQL. See `social-servers.md` for startup, deployment and limits. Hosted network identities are provisioned separately from the Zing login and are marked as hosted in the account list.

## Acceptance sequence

1. Provision two hosted local users; post, follow, reply, like and view a feed without any external account or provider credential.
2. Verify persistence, ownership, moderation, deletion and restart recovery against the real selected server backend.
3. Test ActivityPub between isolated server instances, including discovery, Follow/Accept, posts/replies, updates/deletions, signatures, duplicate delivery and failure recovery.
4. If AT hosting is selected, verify real PDS account/repository/blob operations and synchronization. Validate access through the selected AppView separately.
5. Exercise external federation on configured HTTPS domains before claiming public interoperability. Loopback-only client tests cannot satisfy this gate.

## Primary references

- Mastodon ActivityPub interoperability: https://docs.joinmastodon.org/spec/activitypub/
- AT Protocol hosting and the distinction between PDS, Relay and AppView: https://atproto.com/guides/self-hosting
