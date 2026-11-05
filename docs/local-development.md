# Run Zing locally

The local profile runs the real Go application, PostgreSQL, persistent media storage, and a loopback-only SMTP inbox. It does not contact a cloud mail provider or simulate successful social-network calls.

## First start

Install the Līdza CLI version pinned in go.mod, Python 3, Node and PostgreSQL. Then:

```sh
lidza install
./scripts/local.sh --configure --build
```

`--configure` explicitly selects the local development profile and saves its provider settings encrypted in PostgreSQL. Use it for an empty/local installation; it replaces the instance origin, mail, storage-provider and access choices with local defaults. Without that flag, existing database settings are preserved. `--build` rebuilds the Go binary with its embedded frontend; omit it for an unchanged restart.

Open http://127.0.0.1:3000/setup. Create an operator account and claim the instance using the private token at `config/owner-claim/token`. Verification/reset emails arrive at http://127.0.0.1:8025/; they never leave the machine. Finish setup in the operator pages. Keep PostgreSQL running, and stop the launcher with Ctrl-C to stop its app and mail processes.

For an empty disposable/local instance only, `python3 scripts/bootstrap-local.py` can create, verify and claim a local operator automatically. It refuses to replace an existing owner. The random temporary credentials stay in `.lidza/local-access.json`, mode 0600, and are never printed or committed. Sign in as that account and use Forgot password with the local inbox to choose a new password.

## Daily use

```sh
./scripts/local.sh
```

The app listens on 127.0.0.1:3000, SMTP on 127.0.0.1:1025, and the inbox on 127.0.0.1:8025. These services intentionally bind only to loopback. Messages persist under `.lidza/local-mail/`; media persists under the configured storage directory; accounts, drafts, schedules and settings persist in the local PostgreSQL database. The master key remains at `config/master.key` and must be preserved to reopen encrypted settings.

You can write and save drafts before connecting a social account. Publishing/scheduling requires a supported connected destination. Real Mastodon and Bluesky accounts can be connected from Settings; Bluesky's app-password option supports local development without a public OAuth origin. Threads developer access and HTTPS callbacks remain provider requirements.

The local SMTP inbox has no authentication because it is development-only and loopback-only. Do not expose it through a public proxy, use it for external mail delivery, or include local-access files, master keys, messages or environment files in source archives.
