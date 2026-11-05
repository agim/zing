# Run your own Zing

Zing is open source under Apache-2.0. The source repository being public does not make any hosted instance public. Each operator chooses its domain, host, access policy, storage, and provider credentials. No Albaspot service or credential is required by the application.

## Bootstrap infrastructure

Provide PostgreSQL and a stable 32-byte LIDZA_MASTER_KEY (64 hexadecimal characters). They unlock the database and its encrypted settings; they cannot live only inside that same locked database. Keep the key in a host secret store and back it up separately. Enable AUTH_OWNER_CLAIM=true on a publicly reachable installation. Use Valkey for multiple app nodes, or CACHE_URL=memory on a single node.

Choose Līdza Deploy, the supplied Docker Compose file, or the generated systemd service. Set APP_URL to your actual HTTPS origin. If your host supplies APP_URL, the wizard displays that authoritative value; moving the domain also requires the corresponding DNS/TLS/host change.

## First browser visit

Open `/setup`. Register an operator account, then claim the installation with its one-time owner token. Registration alone grants no operator privileges. On Līdza Deploy, retrieve the hosted app's token through its authenticated Operator setup token action. On another host, retrieve the protected token file from AUTH_OWNER_CLAIM_DIR. Keep the original browser session while configuring mail; the operator's email can be verified after a real mail provider is working.

The operator's first-run page links the installation's domain/access, mail, storage, and optional Threads settings. Choose private/public mode; private mode always permits the owner and permits only exact listed email addresses/administrator IDs for other accounts. It rejects both new unauthorized registrations and existing unauthorized accounts at the application API.

Settings are saved through the framework's encrypted PostgreSQL credential store. Secrets are masked after save. The final setup action validates required choices and snapshots reviewed settings into the database, including any current host defaults. Process environment overrides remain authoritative while present; remove an override and restart to manage that value exclusively from the database. Never put credentials in the repository or a committed deployment profile.

Readiness returns 503 with `Server setup is required` until production setup is finished; a database outage returns a different error and cannot masquerade as setup. Development/test fixtures can explicitly set ZING_REQUIRE_SETUP=false. Finishing the wizard does not substitute for actual email delivery, storage access, or provider-consent checks.

## Storage and mail

Use a private S3-compatible bucket (S3, R2, Spaces, B2, MinIO and supported compatible services), or a persistent local volume. Local media must survive app replacement; container scratch space is not durable storage. Choose your own SMTP service or supported mail provider. Optional Threads credentials belong to your own approved developer app. Mastodon and Bluesky account connections remain per-user and are separate from instance credentials.

The inspected Līdza Deploy source supports managed persistent local storage when no external storage provider is set: it mounts /var/lib/lidza-storage and accepts that durable volume during production validation. This lets the app start the wizard before choosing an object bucket. Leave STORAGE_PROVIDER and object-store credentials out of the host environment to manage them in Zing’s database. The volume remains useful for local storage, while a saved external provider takes precedence at runtime. The hosting agent must support persistent local storage and the configuration/bootstrap contract in deploy/lidza.stack.json; older agents must be updated. Without that support, provide compatible bootstrap storage in the hosting panel first. Standalone Docker/systemd installations need their own persistent writable storage directory.

## Private Albaspot instance

The stack manifest defers mail choices to the guarded wizard, declares an exact setup-readiness marker, and generates a per-app auth signing secret. It never treats a database outage as successful setup readiness. Verify the hosting agent supports this manifest before deployment.

The owner's chosen control panel is https://lidza-deploy.albaspot.com and application origin is https://zing.albaspot.com. `deploy/instances/albaspot.env.example` records ordinary values for that instance only. Select `agim/zing`, branch `main`, attach its database and required infrastructure, use private access, and reveal its setup token through the authenticated panel. The repository remains public. Other installations choose entirely different values.

The control panel is reachable from the implementation environment, but no authenticated deployment session or hosting-agent credential has been supplied. The Zing domain did not complete TLS during the initial read-only check; no successful production deployment is claimed.
