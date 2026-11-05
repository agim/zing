# Hosted social servers

Zing uses GoToSocial 0.22.3 for ActivityPub and the reference @atproto/pds 0.5.52 for AT Protocol. Zing owns application login, installation settings and orchestration; the upstream servers own hosted protocol identities, social records, media and federation. Upstream source is not patched or embedded into the Zing binary.

## Local startup

Keep the existing Go/Līdza application and PostgreSQL installation. In a second terminal, with the pinned Līdza CLI on PATH:

```sh
python3 scripts/social-local.py --install --configure
```

The native installer supports Linux amd64, Python 3.12+, OpenSSL, Node 22+ and Corepack. It verifies the GoToSocial archive against a pinned SHA-256, installs the PDS with the retained upstream pnpm lock, and installs the development PLC package with npm ci. After the first installation, use `python3 scripts/social-local.py`; `--configure` explicitly selects local server settings and replaces existing hosting settings in Zing's encrypted database. Restart the Zing app after changing backend settings:

```sh
./scripts/local.sh --build
```

The launcher starts real servers on loopback: ActivityPub 8081, private provisioner 8091, PDS 2583, isolated PLC 2582. PostgreSQL database `zing_plc` stores the local PLC directory; `ZING_PLC_DATABASE_URL` selects another existing database. It is separate from `zing_dev`. The launcher owns its child processes and stops them on Ctrl-C. Logs and secret state remain ignored under `.lidza/social/`; protocol data survives restarts. Do not run multiple launchers against the same database.

Sign in to Zing, open Settings → Your hosted identities, choose a network and username, then create the identity. No outside social account or provider credential is required. ActivityPub and PDS names need not match. PDS reserved names receive a real error; a backend-confirmed unavailable name can be replaced. Ambiguous outcomes keep their reservation and require retry/reconciliation, never an automatic second identity. Each user can currently host one identity per network.

Use **Write a post** to select that hosted destination, or explicitly select it in Compose. Existing drafts, media preparation, scheduling and per-destination delivery jobs operate on these server-backed accounts. A hosted identity cannot be disconnected like an external connection. Zing account deletion transactionally queues backend deletion using sealed credentials before removing local rows. Incomplete provisioning must be resolved first; unconfirmed deletion jobs remain operator-visible and can require reconciliation.

The local PDS uses `.test` handles and an isolated real PLC service. These development DIDs are not identities on the public PLC directory and must not be advertised as public Bluesky accounts. The local HTTP ActivityPub instance supports local use; its identity domain is permanent. Use separately configured HTTPS servers for real external federation rather than changing the identity origin of populated local databases.

## Local PDS view and full Bluesky services

`ZING_PDS_READ_MODE=local` (default) indexes actual public hosted repositories in PostgreSQL. Following and Discover have separate keyset cursors; search, profiles, counts and local social notifications use the same index. Synchronization commits each source page and resumes incomplete scans after restart. Blocks in either direction, private mutes, suspensions and moderator takedowns filter reads. The former first-100-identities scan has been removed. This is a local community view, not a global Bluesky AppView.

Hosted identities use **Zing chat**, a separate private local service. It never writes message bodies to public repositories or native Bluesky chat. A sender may send one invitation message; the recipient must explicitly accept before either can continue. Membership, suspension and mutual block checks apply to reads and writes. Client UUIDs prevent duplicate sends, bodies are sealed with the installation master key, and realtime events contain owner-scoped invalidation hints only. Chat is not end-to-end encrypted. Externally connected Bluesky accounts retain native DM support when their grant allows it.

A production PDS can proxy read requests to an AppView configured through its upstream `PDS_BSKY_APP_VIEW_URL` and `PDS_BSKY_APP_VIEW_DID` settings. Select `ZING_PDS_READ_MODE=appview` only after those services are configured and accepted with a real hosted account. Relay crawling, moderation/report services, global indexing and chat are separate upstream services, not implied by hosting repositories. Cross-protocol bridging remains separate from hosting both protocols; private messages are never automatically bridged.

## Configuration

Hosting settings can be stored sealed in Zing's PostgreSQL settings store or supplied by its environment:

| Setting | Meaning |
| --- | --- |
| ZING_ACTIVITYPUB_URL | Exact origin of the hosted ActivityPub server |
| ZING_ACTIVITYPUB_AGENT_URL | Private provisioner origin; never public |
| ZING_ACTIVITYPUB_AGENT_TOKEN | Shared provisioner secret |
| ZING_PDS_URL | Exact origin of the hosted PDS |
| ZING_PDS_ADMIN_PASSWORD | PDS admin credential, only used server-side |
| ZING_PDS_HANDLE_DOMAIN | Hosted handle suffix without a leading dot |
| ZING_PDS_READ_MODE | local or appview; default local |

Only exact operator-configured backend origins can use internal network addresses. User-supplied network discovery retains its public-IP-only policy. Redirects are not followed by Zing's backend client. The private ActivityPub provisioner invokes the upstream CLI, confirms only Zing-verified users, and completes OAuth for generated backend credentials; it never receives the user's Zing password. CLI account creation requires a password argument upstream, so the helper belongs in its isolated backend service, not a shared multi-user shell. Its journal contains operation IDs and account identifiers, not passwords or tokens. Provider access/refresh tokens and backend passwords are encrypted in Zing's database.

## Container deployment

The container overlay is prepared; Docker is unavailable on the current development host, so container build/start and public-domain acceptance remain unverified.

Choose independent ActivityPub and PDS domains (for example `social.example.org` and `pds.example.org`) alongside the Zing interface. Populate `.env.production` with:

```dotenv
ZING_ACTIVITYPUB_DOMAIN=social.example.org
ZING_ACTIVITYPUB_AGENT_TOKEN=<random-shared-secret>
ZING_PDS_DOMAIN=pds.example.org
PDS_ADMIN_PASSWORD=<random-secret>
PDS_JWT_SECRET=<random-secret>
PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX=<32-byte-secp256k1-private-key-hex>
ZING_PDS_READ_MODE=local
```

Generate real random values locally; never commit the files. Create an ignored `.env.pds.production` for upstream PDS SMTP, AppView, crawler, report-service and storage options (it may be empty for repository-only hosting). This service file belongs to the upstream PDS; Zing application SMTP/storage configuration remains independent and stored through its existing first-run settings.

```sh
docker compose --env-file .env.production -f deploy/compose.yaml -f deploy/compose.social.yaml up --build -d
```

Configure HTTPS reverse proxies using `deploy/Caddyfile.social.example`, and configure the PDS domain/wildcard DNS and TLS as required by upstream. The ActivityPub provisioner is available only on the container network; do not publish 8091. Its configured public ActivityPub origin must be reachable from the backend container for OAuth. Do not use the upstream PDS machine-wide installer on a host already serving Zing: the overlay keeps services independent. No public DNS, server installation, or Līdza Deploy configuration has been changed here.

Back up Zing PostgreSQL and its master key, the ActivityPub database/media/journal volume, the PDS account/sequencer/DID-cache databases, actor repositories, blobs and signing secrets. Use upstream-consistent database snapshots or stop the services before volume snapshots; copying a live SQLite main file without its WAL is not a complete backup. Local development additionally needs the `zing_plc` database. Restore and test the entire identity set before resuming traffic. Registration privacy is distinct from federation visibility: public posts can leave the server.

The verified operator’s `/admin/moderation` page handles reports, suspension/restoration, reported hosted-post takedowns, and ActivityPub domain blocks. Every action records an operator, reason and timestamp; durable jobs record per-network confirmation. Native GoToSocial disable/enable is followed by a supervisor-confirmed restart because its account cache otherwise preserves old permissions. PDS administration applies/reverses native repository takedown status. The protected `zingmoderator` service identity supplies only backend administration; its generated password and token are sealed server-side. Ordinary users cannot reserve this name. Use the Jobs admin page to inspect/retry exhausted jobs; failures are never displayed as success.

Suspended users can submit one open appeal and can still delete their accounts. Deletion removes private chat bodies authored by the deleting user, membership, indexed records, notifications, preferences, own reports and local credentials. Native cleanup persists independently; a disabled ActivityPub account uses the destructive native erasure path only after the user requested deletion. GoToSocial 0.22.3 deliberately retains an email and actor/user tombstones after native deletion. The private agent verifies that the native actor was stubbified last, then replaces the retained email with a random operation alias and restarts to discard cached data. This narrowly scoped SQLite supplement is tied to the pinned reference server; it never alters an active account. New managed ActivityPub accounts receive anonymous service aliases from the outset. The agent then removes the provisioning journal’s email data. A conversation’s remaining participant retains their own messages, with the other participant marked deleted. Minimal moderation action metadata remains as an operator audit trail. Backend credentials/settings alone do not count as successful live public-network acceptance.

## Acceptance

At module completion, run `lidza test`, `lidza test --e2e`, and `lidza verify --full`. Real isolated federation acceptance is separate:

```sh
python3 scripts/verify-federation-local.py
```

The script creates two disposable upstream servers with a local TLS certificate, validates certificates using its local trust root, and exercises discovery plus signed Follow/Accept, Create, Like and Delete. Both listeners bind to loopback and the temporary state is removed afterward. It does not disable production TLS verification or create public PLC identities. `python3 scripts/verify-hosting-local.py` exercises hosted identity creation and persistent publication in the actual local Zing browser journey (requires Python Playwright and `.lidza/local-access.json`). It refuses non-local backend domains and creates explicitly labeled fixture posts on the disposable instance. Screenshots stay in `.lidza/hosting-acceptance`. Fixture-only provider tests do not replace this evidence.

## Upstream references and licenses

- GoToSocial source/releases and AGPL-3.0-or-later: https://codeberg.org/superseriousbusiness/gotosocial
- GoToSocial authentication and CLI: https://docs.gotosocial.org/en/latest/api/authentication/ and https://docs.gotosocial.org/en/latest/admin/cli/
- Reference PDS service and pinned dependency lock: https://github.com/bluesky-social/pds/tree/main/service (MIT/Apache licenses as supplied upstream)
- AT Protocol component boundaries: https://atproto.com/guides/self-hosting
- Local PLC package source: https://github.com/did-method-plc/did-method-plc (MIT)

Container images retain their upstream binaries/licenses. Zing's own application license does not replace upstream licensing.

## Current release holds

The service audit on 2026-11-05 uses PDS 0.5.52 and maintained unbundled PLC source at `29f1a70ee9f6f80a25946cac103b2f745e371334`. `services/vendor/provenance.json` pins archive hashes; archives retain upstream TypeScript source and MIT license. The local PLC migration preserves existing DIDs and signing keys; newer DID documents use Multikey for the same verification key.

Run `python3 scripts/verify-service-security.py` after installing both service locks. It checks actual resolved code, archive/patch hashes and fresh production dependency audits. PDS retains two upstream **version alerts** (Zod high, deepmerge low); local PLC retains the Zod alert. Compatible code backports bound Zod error retention and reject prototype-control merge keys. Regression tests exercise the installed code. Unknown alerts or changed backports fail the script. This is documented backport evidence, not an upstream clean-audit claim. Zod 4 breaks PDS OAuth compatibility, so it is not substituted merely to silence a version scanner.

`lidza check --security` includes Go vulnerability scanning, Rust advisory/license checks and the frontend audit. The Rust/WASM media pack fully decodes bounded still images, applies orientation, resizes to 1400px, strips original metadata and emits canonical PNG. Animated/damaged uploads are rejected. The pack has no network/filesystem access, a 256 MiB memory ceiling and a five-second deadline.

Local acceptance: `scripts/verify-hosting-local.py`, `scripts/verify-community-local.py`, and `scripts/verify-federation-local.py`. The community script creates a labeled temporary user, exercises real native administration and restores the installation’s private registration policy. Keep tests on feature/module boundaries; run the complete suite and browser gate before a version release.

Public deployment still needs container runtime validation, real TLS/domains, outbound federation and AppView credentials/configuration where desired, SMTP/storage choices, backup restoration and traffic/rollback checks. Local PLC is loopback-only and excluded from the production stack. Threads still requires an approved Meta developer app; unavailable operations remain explicit.
