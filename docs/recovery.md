# Encrypted instance recovery

Back up all writers at one quiescent boundary: Zing workers, ActivityPub and its
agent, PDS and a local PLC directory. Never copy an actively written SQLite/WAL
repository and assume it is consistent. Preserve the immutable identity domains.
For S3, take and inventory a corresponding object-storage snapshot/version set.

`scripts/backup-instance.py` needs Python 3.12+, age, psql, and pg_dump/pg_restore
matching each PostgreSQL server's major version. It accepts a private JSON file:

```json
{
  "databases": {"app": "postgresql://user:password@host/zing"},
  "paths": {
    "configuration": "/private/zing/config",
    "environment": "/private/zing/.env.production",
    "activitypub": "/private/zing/activitypub-data",
    "pds": "/private/zing/pds-data",
    "objects": "/private/zing/storage"
  }
}
```

Add the PLC database and signing/provisioning keys when locally hosted. Include
LIDZA_MASTER_KEY, AUTH_SECRET and native keys through a private configuration or
environment path; a database cannot decrypt itself without the original key.
Never commit this config, dumps, keys or private data. Keep the independent age
identity offline, outside the backup. A backup recipient is public.

```sh
age-keygen -o /offline/zing-recovery.agekey
age-keygen -y /offline/zing-recovery.agekey
python3 scripts/backup-instance.py backup --config /private/backup.json \
  --out /backups/zing.tar.gz.age --recipient age1YOUR_PUBLIC_RECIPIENT --quiesced
python3 scripts/backup-instance.py restore --archive /backups/zing.tar.gz.age \
  --identity /offline/zing-recovery.agekey --expected-sha256 TRUSTED_INVENTORY_HASH \
  --destination /isolated/zing-restored
```

Retain the returned SHA-256 in an authenticated backup inventory stored separately
from the backup object. Anyone knowing a recipient can encrypt new data to it;
encryption alone does not authenticate the snapshot's operator. Restore rejects
an inventory mismatch before decrypting, rejects unsafe archive members, verifies
every manifest hash, and only then exposes the new directory. It refuses an
existing restore destination. It never replaces a running database or live data.

Restore logical dumps to **new** databases with `--no-owner --no-acl --exit-on-error`.
Compare every table count with manifest counts and run native SQLite integrity
checks. Restore files with the runtime UID/GID (the generic safe extractor does
not adopt archived owners). Docker acceptance uses private volume tar payloads to
preserve numeric owners. Restore keys/config and objects from the same boundary.
Start the isolated app with `JOBS_WORKERS=0`; prevent external egress. Confirm
readiness, actual login, owned connections, credential decryption, private media,
DID/repository/post/blob continuity before considering traffic or workers.

`python3 scripts/verify-recovery-local.py` rehearses the native local profile into
fresh PostgreSQL databases and a new directory, leaving original state untouched.
It requires the primary local launchers to be stopped. It verifies all file hashes
and counts, six SQLite stores, a wrong master key, corrupted ciphertext, native PDS
login/repository continuity, application login and disabled workers. Its outputs
are private under `.lidza/recovery/`; the recovery age key is not an app credential.

`python3 scripts/verify-containers-local.py` creates disposable Compose projects,
registers a real fixture through SMTP and native servers, injects an unavailable
PDS/restart, publishes real media, and restores encrypted snapshots into fresh
volumes/databases. Requires Docker Compose 2.24+ and age. The fixture's internal
network has no public egress and no public identities. It never uses production
`.env` files. It cleans only its uniquely named fixture projects and volumes.

A restore is a recovery boundary, not a normal binary rollback: reconcile uncertain
publications before enabling workers to avoid duplicating upstream posts. Keep a
previous verified application image for compatible application rollback; never
run destructive down migrations automatically. Keep routine encrypted off-host
backup retention and repeat drills after schema/native-server upgrades.
