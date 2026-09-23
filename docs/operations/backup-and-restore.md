# Backup and restore

Written for whoever runs a Pivot instance. It assumes you can reach a shell on
the machine and nothing else.

## The short version

```sh
pivot backup                      # writes pivot-20260923T120000Z.db
pivot restore pivot-20260923T120000Z.db
```

Both read the same configuration `pivot serve` does, so inside a container or
a systemd unit they need no arguments.

**Restore with the server stopped.** Backup is safe to run at any time;
restore is not. See [Why restore needs the server
stopped](#why-restore-needs-the-server-stopped).

## What `pivot backup` does, and why not `cp`

It runs SQLite's `VACUUM INTO`, which takes its copy inside a read transaction.
Writers are not blocked, and what lands on disk is the database as of one
instant.

Copying the file is not equivalent, and the difference is not academic. Pivot
runs SQLite in WAL mode, which means the database is **three** files:

```
pivot.db        the database
pivot.db-wal    committed transactions not yet folded back in
pivot.db-shm    the index into that WAL
```

`cp pivot.db backup.db` copies the first one. Recent commits live in the
second, so the copy is missing them. Copying all three is no better: they are
copied one after another, the database changes in between, and the result is a
WAL that does not match the database it is supposed to belong to. SQLite
reports that as `database disk image is malformed` — months later, when
somebody needs the backup and has no other copy.

`VACUUM INTO` also rebuilds the file, so the backup is compact.

The backup is written `0600`. It contains every session token hash and every
stored secret in the instance, and inheriting the process umask would leave it
readable by anything on the machine.

### PostgreSQL

Not supported by these commands, on purpose. `pg_dump` and `pg_restore` exist,
everybody running PostgreSQL already has them, and a worse reimplementation
inside this binary would produce something that looks like a backup.

```sh
pg_dump --format=custom --file=pivot.dump "$PIVOT_DATABASE_URL"
pg_restore --clean --if-exists --dbname="$PIVOT_DATABASE_URL" pivot.dump
```

## Restoring

```sh
systemctl stop pivot          # or: docker compose stop pivot
pivot restore pivot-20260923T120000Z.db
systemctl start pivot
```

What it does, in order:

1. **Checks the backup first.** It opens it and reads its schema version. A
   file that is not a Pivot database is refused here, before anything has been
   moved — the dangerous version of this command discovers the problem after
   renaming the live database aside, leaving you with no database at all and
   fewer options than you started with.
2. **Renames the live database aside** to `pivot.db.replaced-<timestamp>`
   rather than deleting it. Restoring the wrong file is a mistake people make
   at three in the morning, and it must not be the end of the story. Pass
   `--force` to delete instead.
3. **Copies the backup into place**, `0600`.
4. **Removes the stale `-wal` and `-shm` files.** They belong to the database
   that was just replaced. Left behind, SQLite applies one database's journal
   to another, which corrupts the thing you just restored.

Then check it:

```sh
pivot doctor
```

### Why restore needs the server stopped

A running Pivot holds an open descriptor on the database file. Renaming the
file does not take it away: the process keeps reading and writing the old
contents through the descriptor it already has, and then writes them back over
your restore. The restore appears to succeed and silently does nothing.

There is no way for the command to detect this reliably, so it does not
pretend to. Stop the server.

## What is in the backup, and what is not

**In:** organizations, users, password hashes, groups, role assignments,
sessions, identity-provider configuration.

**Not in:** the configuration file, any environment variables the instance
needs, and **the encryption key**. A backup restores the *data*; it does not
restore an instance.

The key's absence is deliberate. Stored secrets are encrypted with it, so a
backup containing both is exactly as exposed as one with no encryption at all.
Back the key up separately and store it somewhere these backups are not —
see [secrets.md](secrets.md). Restoring a database without its key gives you an
instance whose stored secrets cannot be read; everything else works.

## A schedule

There is no built-in scheduler yet — Phase 5 owns background work. Until then
this is a cron line:

```cron
# Nightly at 03:00, keeping 14 days.
0 3 * * * cd /var/lib/pivot && pivot backup -o "/var/backups/pivot/pivot-$(date -u +\%Y\%m\%dT\%H\%M\%SZ).db" && find /var/backups/pivot -name 'pivot-*.db' -mtime +14 -delete
```

`pivot backup` refuses to overwrite an existing file, so a job that runs twice
fails the second time rather than replacing a good backup with a partial one.

## Test the restore

A backup nobody has restored is a hypothesis. Once, on a copy:

```sh
mkdir /tmp/pivot-restore-test && cd /tmp/pivot-restore-test
PIVOT_DATABASE_URL=sqlite://./pivot.db pivot restore /var/backups/pivot/pivot-20260923T120000Z.db
PIVOT_DATABASE_URL=sqlite://./pivot.db pivot doctor
PIVOT_DATABASE_URL=sqlite://./pivot.db pivot serve --port 8099
```

Open it, sign in, and confirm your own account is there. That is the whole
test, and it is the only one that answers the question you will actually be
asking when it matters.
