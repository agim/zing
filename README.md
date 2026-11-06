# Zing

Your world, connected. Zing is a self-hosted social platform with a Go + Līdza application and React/TypeScript frontend, using the supplied Zing logos and #0085F3 brand palette.

Zing hosts social identities through pinned GoToSocial and Bluesky PDS backends, alongside optional external-account connections. See `docs/social-servers.md` for local startup, protocol boundaries and deployment. Local hosted Bluesky feeds use a persistent community index. Hosted identities have clearly labeled, consented Zing chat; external Bluesky accounts retain native DMs. Operator moderation changes the actual hosted servers. A global Bluesky AppView/Relay remains a separate optional service.

Private interactive design: https://zing-design-agim.albaspot.chatgpt.site (sample data only). Its complete source is retained in `design/dist`; production uses `src/` and Go handlers.

## Self-host

Use Līdza Deploy, Docker, or your own host/domain. Open `/setup` to claim the operator account and choose private/public access, mail, storage, and optional provider credentials. Settings are encrypted in PostgreSQL. The public repository and the privacy of a hosted instance are independent. See `docs/self-hosting.md` for bootstrap requirements and the private Albaspot deployment profile.

## Develop

Install the Līdza CLI at the version in go.mod and Rust 1.99.1 with the `wasm32-wasip1` target for the contained media pack, then run:

```sh
lidza doctor
lidza install
lidza credentials init
lidza dev
```

For a configured local app with a private operator and real loopback SMTP inbox, see `docs/local-development.md` (`./scripts/local.sh --configure --build` after installation).

Use PostgreSQL locally; `.env.example` lists the settings. For local-only development set CACHE_URL=memory, MAIL_PROVIDER=outbox, and STORAGE_PROVIDER=local. Email verification is required before connecting accounts. Development verification emails can be inspected through Līdza's mail outbox/operator tools. Production must use real email delivery.

Mastodon uses instance-specific OAuth. Bluesky defaults to OAuth on a public HTTPS origin; an explicit app-password connection works for local development and requires chat access for messages. Threads requires developer-app configuration and approved permissions. No production screen fabricates provider success.

## Build and verify

```sh
lidza test              # at feature/module completion
lidza verify --full     # before version release/commit
lidza test --e2e        # browser release checks
lidza build            # bin/zing with embedded frontend
python3 scripts/verify-service-security.py # installed companion dependency checks
python3 scripts/verify-agent-security.py   # native erasure privacy regression
```

See `docs/platform-alignment.md` for implemented network behavior and limits, `docs/operations.md` for deployment, backups, rollback and the public-launch gate, and `docs/brief.md` for the agreed scope. This repository contains the application implementation; live provider acceptance and public deployment still require operator-owned accounts and infrastructure.

New signups enroll automatically on configured hosted ActivityPub and AT Protocol servers after email verification. Settings shows per-network progress and supports recovery without recreating ready identities. Existing installations keep their pre-existing identities. See `docs/recovery.md` for encrypted snapshots and isolated restore drills.

Local two-network container acceptance and encrypted fresh-volume/database recovery
are covered by `scripts/verify-containers-local.py`. See `docs/recovery.md` for
backup operations and `docs/container-security.md` for the complete image review,
exact residual findings and the release scan gate. Local validation does not
configure or deploy a production host.
