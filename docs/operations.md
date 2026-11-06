# Deployment and release

## Configuration

Run `lidza doctor`, `lidza install`, and `lidza credentials init`. Keep the CLI at the version pinned in go.mod. Never commit .env files or config/master.key. The generated encrypted credentials file is environment-specific; provision fresh production credentials instead of copying development values.

Production needs PostgreSQL, Valkey, an HTTPS origin, an SMTP service, private object storage (a persisted local volume or S3-compatible storage), and a stable LIDZA_MASTER_KEY. Configure THREADS_CLIENT_ID and THREADS_CLIENT_SECRET only in sealed settings or the process environment. Meta app review and approved scopes are a launch dependency. Register the documented callback URI exactly. Bluesky OAuth metadata must be publicly readable on APP_URL; app passwords are an explicit local-development alternative.

The Dockerfile builds the embedded frontend and Go binary. `deploy/compose.yaml` provisions the app, PostgreSQL and Valkey; place a TLS reverse proxy in front of localhost:3000. For an external proxy, configure LIDZA_TRUSTED_PROXIES to its actual address/CIDR. The supplied Caddyfile assumes Caddy is attached to the Compose network; adapt its upstream if Caddy runs on the host.

Apply migrations with `lidza db migrate --production` or DB_MIGRATE=true. All nodes share the database, master key, auth secret, and Valkey. Keep owner-claim state private and complete the one-time administrator claim; do not let the first visitor become an operator. Public API requests are limited per account; credential routes have separate throttles.

## Release gate

Regular tests run at completed module boundaries, followed by `lidza verify --full` and `lidza test --e2e` at a version release. CI runs on release tags, manual dispatch and PRs, not each feature edit. Never bypass the pre-commit verification gate.

Before opening registration: configure and test SMTP delivery, complete actual consent and revoke/reconnect journeys on all providers, exercise image publishing and private messages with operator-owned accounts, verify requested scopes, and finish the privacy/terms/contact and abuse-reporting policies. The Sites design is private and uses sample data; its successful deployment does not verify the Go production app.

## Operations

Check /healthz and /readyz before switching traffic; keep /metrics, /mcp and debug routes behind operator access. Alert on failed/uncertain delivery counts, growing job backlog, provider 429/5xx responses and database/storage failures. Logs must contain neither credentials nor message bodies. Background synchronization selects at most 50 connections per sweep; monitor queue age before increasing workers.

A sending delivery left by a crashed worker becomes uncertain on job recovery. Confirm its original network post before using reconciliation. A 5xx or transport timeout cannot prove non-publication. Cancellation stops pending work; a provider request already in flight can finish. Successful destinations are never requeued by a failed destination's Retry action.

## Backups and rollback

Take encrypted PostgreSQL backups (including job/session rows) and object-storage snapshots/versioning. Store LIDZA_MASTER_KEY in an independent secret backup; losing it makes connected credentials unusable. Rehearse restoring the database and objects into an isolated environment with outbound publishing workers disabled (JOBS_WORKERS=0), compare counts and sample owned records, then reconnect provider test accounts before enabling workers.

Keep the previous application image/binary. Deploy additive migrations before traffic switch, check /readyz, then roll forward. On application failure, route traffic to the previous binary while preserving compatible database state; never automatically run destructive down migrations. Reconcile in-flight publication jobs before restoring a backup or resuming workers.

The owner selected lidza-deploy.albaspot.com and zing.albaspot.com for a private instance. Other installers choose their own host and domain. Authenticated deployment access, initial infrastructure and provider approvals remain installation requirements; see docs/self-hosting.md.

## GitHub Actions installation

The repository connection used for the initial push cannot create workflow files (GitHub requires the OAuth `workflow` scope). The full release/PR configuration is retained at `deploy/github-ci.yml`. An account with workflow-write permission can copy it to `.github/workflows/ci.yml`. Local Līdza verification and browser tests remain the release gate in the meantime.

## Hosted server acceptance

This installation also owns ActivityPub and PDS identities. Follow `social-servers.md` for companion service startup, immutable identity domains, backend backups, deletion jobs and actual isolated federation acceptance. The current `lidza.stack.json` covers the Zing application infrastructure; deploy its companion servers independently and configure their origins/secrets, or use the prepared Compose overlay. It does not yet install all protocol services through Līdza Deploy automatically. See `container-security.md` for the current image/backport review and remaining release holds. Disposable two-network container acceptance and fresh-volume recovery have passed locally. This does not substitute for the configured production host, HTTPS origins, external SMTP delivery, provider credentials or Meta review.

For the complete encrypted native/app recovery boundary, tooling and isolated drills, see `recovery.md`. Never restore application rows without the matching native repositories, blobs and keys.


Local acceptance commands and results are recorded in `recovery.md` and
`container-security.md`. PostgreSQL health requires a successful authenticated TCP
query so the temporary initialization server cannot falsely satisfy readiness.
The container fixture uses an internal network with loopback test ingress and
never creates a public-network identity. No production deployment was performed.
