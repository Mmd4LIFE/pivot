# Installing Pivot

Two ways, and they are genuinely different choices rather than the same thing
packaged twice.

| | Single binary | Container stack |
|---|---|---|
| Database | SQLite, created for you | PostgreSQL, in the stack |
| Dependencies | none | Docker |
| Good for | a laptop, a small team, trying it out | a server, several people, growing |
| Backup | `pivot backup` | `pg_dump` |
| Time to first login | about 30 seconds | about two minutes, most of it pulling |

Both run the same binary. Moving between them is a database migration, not a
reinstall.

## The single binary

Download the release for your platform from
[the releases page](https://github.com/Mmd4LIFE/pivot/releases), then:

```sh
tar xzf pivot_*_linux_amd64.tar.gz
./pivot serve
```

That is the whole thing. No configuration file, no database to create, no
arguments. It creates `pivot.db` beside itself, applies its migrations, writes
an encryption key to `pivot.key`, and prints a setup token:

```
  This Pivot has no administrator yet.

    Open:  http://localhost:8080/setup
    Token: EXAMPLE-TOKEN-yours-will-be-random
```

Open that URL, paste the token, fill in the form, and you are inside Pivot as
its administrator.

### Verifying the download

Releases are signed with [cosign](https://docs.sigstore.dev/) using keyless
Sigstore, so there is no public key to fetch and trust:

```sh
cosign verify-blob \
  --bundle pivot_0.0.1-alpha_linux_amd64.tar.gz.cosign.bundle \
  --certificate-identity-regexp 'https://github.com/Mmd4LIFE/pivot/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  pivot_0.0.1-alpha_linux_amd64.tar.gz
```

That says the file was built by this repository's release workflow, from this
repository's source, and has not changed since.

### Afterwards

```sh
pivot doctor    # is anything wrong with this install?
pivot backup    # a consistent copy, safe to run while serving
```

Two things worth doing on a machine other people can reach:

- **Move `pivot.key` somewhere the database backups do not go.** It is what
  makes a stolen database useless, and a backup containing both is exactly as
  exposed as one with no encryption at all. `pivot doctor` warns while the two
  share a directory. See [secrets.md](secrets.md).
- **Put TLS in front of it**, and set `auth.cookieSecure`. Pivot serves plain
  HTTP and does not terminate TLS itself.

## The container stack

```sh
git clone https://github.com/Mmd4LIFE/pivot
cd pivot/deploy

cp .env.example .env
# fill in the three required values; the file says what each one is
docker compose up -d
```

Then find the setup token:

```sh
docker compose logs pivot | grep -A2 'no administrator yet'
```

and open http://localhost:8080/setup.

`.env` needs three values and refuses to start without them: a database
password, an encryption key, and a setup token. Each is either a secret or the
thing that protects your secrets, and a default would be the same on every
Pivot anybody ever ran.

### What is in the stack

Pivot and PostgreSQL. Both have health checks, and Pivot waits for the database
to be *ready* rather than merely listening — it migrates on startup, and a
migration against a PostgreSQL that is still starting fails.

There is deliberately no Valkey and no MinIO yet, though the plan named them:
nothing in Pivot connects to them until Phase 9 and Phase 5 respectively, and a
compose file that starts services the product never talks to teaches operators
they are required.

PostgreSQL publishes no port. It is reachable from Pivot and from nowhere else;
for a shell, use `docker compose exec postgres psql -U pivot`.

### Backups

`pivot backup` is for SQLite. For this stack:

```sh
docker compose exec postgres pg_dump -U pivot --format=custom pivot > pivot.dump
```

And keep `PIVOT_SECRETS_KEY` somewhere else entirely — see
[backup-and-restore.md](backup-and-restore.md).

## Upgrading

Single binary: stop it, replace the file, start it. Migrations run on startup.
Take a backup first; that is the only rollback there is.

Container: change `PIVOT_VERSION` in `.env`, then `docker compose up -d`. The
tag is pinned rather than `latest` on purpose — an image tag that moves means a
restart during an incident can also be an upgrade, and nobody finds out until
afterwards.

## Behind a proxy

Pivot serves HTTP and expects something in front of it for TLS. Set
`server.baseURL` to the address a browser actually uses: it is what OIDC
redirect URIs are built from, so the container's own address will not do.

`X-Forwarded-For` is **not** trusted yet — rate limiting keys on the socket
address, which behind a proxy is the proxy. Phase 9's deployment work fixes
that. Until then, per-IP limits behind a reverse proxy are effectively global,
which is worth knowing before you rely on them.
