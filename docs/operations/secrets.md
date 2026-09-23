# Stored secrets and the key that protects them

Written for whoever runs a Pivot instance.

Pivot stores some values that must not be readable from the database alone —
today an OIDC client secret, and from Phase 1 the credentials for your
warehouses. They are encrypted with a master key that lives outside the
database.

## The short version

```sh
pivot secrets status        # what is stored, and under which key
pivot secrets rewrap        # move everything onto the current key
pivot secrets generate-key  # print a new key, for provisioning
```

**Keep the key somewhere the database's backups do not go.** That is the whole
point: a backup that contains both is exactly as exposed as one with no
encryption at all.

## Where the key comes from

In order:

1. **`secrets.key` / `PIVOT_SECRETS_KEY`** — the key itself, base64. This is
   the right answer under an orchestrator, which already has a way to deliver a
   secret to a process.
2. **`secrets.keyFile` / `PIVOT_SECRETS_KEY_FILE`** — a file to read it from.
3. **A generated key**, written to `pivot.key` beside the database, if none of
   the above exists.

The third is what makes a zero-config first run encrypt anything at all, and it
is a real compromise: a key beside the database protects a stolen database, a
stolen backup and a decommissioned disk, and does not protect a stolen machine.
`pivot doctor` warns when the key and the database share a directory.

Refusing to start until somebody provisions a key would protect against more.
It would also mean nobody gets past the first run, so it is not what Pivot
does.

## What this protects against, and what it does not

**It protects against somebody obtaining the database.** A stolen backup, a
snapshot left on object storage, a decommissioned disk, a `SELECT *` by a
colleague with read access to the metadata store. In all of those the stored
value is an envelope and useless without the key.

**It does not protect against somebody obtaining the machine.** Pivot decrypts
on demand, so it holds the key in memory and reads it at startup. Root on that
host can have the plaintext. Defending against that needs a KMS or an HSM,
which is Phase 9's job — the envelope format carries a key identifier so that a
different key source is a new implementation rather than a migration.

## What a stored secret looks like

```
pivot.v1.c78cccd1.DnBhqD8ewykhmpTW...Kesd.zLgLVy5ZXiyfWlh7kDtk...dlJa
└───┬──┘ └───┬──┘ └────────┬──────────┘ └──────────┬──────────┘
  format    key id    wrapped data key          ciphertext
```

Each value gets its own random data key; the data key is wrapped by the master
key. Rotation rewraps data keys rather than re-encrypting data, which is the
same amount of work whether you have five secrets or five thousand.

The value is self-describing on purpose: an operator looking at a row can tell
an encrypted secret from a plaintext one without asking anybody, and the key
identifier says which key is needed. The identifier is a fingerprint of the key
material, so two instances given the same key agree on its identity without
being told.

## Rotating the key

**Three steps, in this order.** The middle one is not optional, and the order is
not negotiable: a secret can only be read by the key that sealed it.

```sh
# 1. New key primary, old key retained for reading.
export PIVOT_SECRETS_KEY="$(pivot secrets generate-key)"
export PIVOT_SECRETS_PREVIOUS_KEYS="<the old key>"

pivot secrets status        # "encrypted with an older key"

# 2. Move everything onto the new key.
pivot secrets rewrap

pivot secrets status        # "encrypted with the current key"

# 3. Only now, drop the old key.
unset PIVOT_SECRETS_PREVIOUS_KEYS
```

Restart the server after step 1 and again after step 3, so the running process
has the same keys the commands do.

**If you do step 3 before step 2, the secrets are gone.** Not corrupted,
not recoverable — the key that could read them no longer exists. Keep the old
key until `pivot secrets status` reports nothing on it.

`pivot secrets rewrap --dry-run` reports what it would change and changes
nothing.

## Upgrading from a version before encryption

Nothing breaks. Secrets stored as plaintext are read as plaintext, so SSO keeps
working through the upgrade. They are reported as unencrypted until you seal
them:

```sh
pivot secrets status        # "1 not encrypted"
pivot secrets rewrap
```

## If the key is lost

There is no recovery. That is what it means for the encryption to work.

What survives: everything else in the database — users, groups, roles,
dashboards. What is lost: the stored secrets themselves. For an OIDC provider
that means going to the identity provider, issuing a new client secret, and
running `pivot admin add-provider` again with it.

If a key file exists but cannot be read — a truncated write, the wrong file —
**do not delete it and start again.** Deleting it is what turns "the key looks
wrong" into "the secrets are gone". Find the right file first.

## Backups

The key is **not** in a `pivot backup`, and that is deliberate: a backup
containing both is as exposed as an unencrypted one.

Back the key up separately, once — it does not change unless you rotate it — and
store it somewhere the database backups are not. A password manager, your
orchestrator's secret store, or a sealed envelope in a drawer all beat the same
S3 bucket.

Restoring a database without its key gives you an instance whose secrets cannot
be read. `pivot doctor` says so, and `pivot secrets status` counts them.
