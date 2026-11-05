# Zing

Your world, connected. Zing is intended to be a self-hosted social platform with a Go + Līdza application and React/TypeScript frontend, using the supplied Zing logos and #0085F3 brand palette.

Zing hosts social identities through pinned GoToSocial and Bluesky PDS backends, alongside optional external-account connections. See `docs/social-servers.md` for local startup, protocol boundaries and deployment. The local PDS view is not a global Bluesky AppView or chat service.

Private interactive design: https://zing-design-agim.albaspot.chatgpt.site (sample data only). Its complete source is retained in `design/dist`; production uses `src/` and Go handlers.

## Self-host

Use Līdza Deploy, Docker, or your own host/domain. Open `/setup` to claim the operator account and choose private/public access, mail, storage, and optional provider credentials. Settings are encrypted in PostgreSQL. The public repository and the privacy of a hosted instance are independent. See `docs/self-hosting.md` for bootstrap requirements and the private Albaspot deployment profile.

## Develop

Install the Līdza CLI at the version in go.mod, then run:

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
```

See `docs/platform-alignment.md` for implemented network behavior and limits, `docs/operations.md` for deployment, backups, rollback and the public-launch gate, and `docs/brief.md` for the agreed scope. This repository contains the application implementation; live provider acceptance and public deployment still require operator-owned accounts and infrastructure.
