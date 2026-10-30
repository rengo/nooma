# 08 — Running Nooma on a server

Nooma runs 24/7 on a Linux host in Docker, reachable only over Tailscale, with a daily
encrypted backup to Google Drive. The files are under [`deploy/`](../deploy); the backup
decision is [ADR-0049](adr/0049-backup-is-a-built-in-snapshot.md).

| On the host | What it is |
|---|---|
| `/opt/nooma` | the git clone, for the compose files and the backup script; `deploy/docker/.env` holds the secrets (gitignored) |
| `/srv/nooma/data/vault` | the vault (`/data/vault` in the container) |
| `/srv/nooma/backups` | the local backups (`/backups` in the container) |

Nooma is never public. It has its own compose project and network, no `VIRTUAL_HOST`, is never
on the `proxy` network, and its port is published on `127.0.0.1` only. Telegram is long polling,
so no inbound port is needed.

## 1. Install and start

```sh
sudo git clone https://github.com/rengo/nooma /opt/nooma && cd /opt/nooma
sudo install -d -o 65532 -g 65532 -m 0700 /srv/nooma/data /srv/nooma/backups
sudo cp deploy/docker/.env.example deploy/docker/.env && sudo chmod 600 deploy/docker/.env
```

The image runs as uid 65532 (distroless `nonroot`), so both directories belong to it. It is the
published release, `ghcr.io/rengo/nooma` ([ADR-0060](adr/0060-releases-are-built-by-goreleaser-on-a-tag.md)),
for `linux/amd64`, `arm64` and `arm/v7`: `latest` by default, the newest release that is not a
release candidate, or the one `NOOMA_VERSION` names in `deploy/docker/.env` (`0.5.0`, no `v`).
Nothing is compiled on the host. To run unreleased `main` instead, see
[Building from source](#building-from-source). Until the first release is published there is
no image to pull: build from source, and see
[Upgrading from a source build](#upgrading-from-a-source-build).

### The env file

`deploy/docker/.env` carries the values `nooma.yml` names (the container's environment wins over
the vault's own `.env`):

| Variable | Named by `nooma.yml` key |
|---|---|
| `OPENAI_API_KEY` | `providers.*.api_key_env` |
| `TELEGRAM_BOT_TOKEN` | `channels.telegram.bot_token_env` |
| `NOOMA_AUTH_TOKEN` | `server.auth_token_env`; generate with `openssl rand -hex 32` |

The token is mandatory: inside the container serve binds `0.0.0.0` so the port mapping reaches
it, and a non-loopback bind without a token refuses to start (ADR-0007). The allowed chat ids
are not secret and live in `nooma.yml`. The zone is `TZ` in `compose.yml`.

### First run: create the vault

```sh
docker compose -f deploy/docker/compose.yml pull
docker compose -f deploy/docker/compose.yml run --rm nooma init /data/vault
```

The wizard asks for a provider. Then edit `/srv/nooma/data/vault/nooma.yml`:

```yaml
server:
  bind: 0.0.0.0
  http_port: 7777
  auth_token_env: NOOMA_AUTH_TOKEN
channels:
  telegram:
    enabled: true
    bot_token_env: TELEGRAM_BOT_TOKEN
    allowed_chat_ids: [123456789]
```

```sh
docker compose -f deploy/docker/compose.yml up -d
docker compose -f deploy/docker/compose.yml logs -f nooma   # "nooma: listening on 0.0.0.0:7777"
```

There is no container healthcheck: the image has no shell or HTTP client, and the binary has
no probe command. `restart: unless-stopped` restarts it if it exits.

### Check the vault: doctor and status

Run them through the container, so they run as the user serve runs as (uid 65532):

```sh
docker compose -f deploy/docker/compose.yml exec nooma nooma doctor /data/vault    # while Nooma runs
docker compose -f deploy/docker/compose.yml run --rm nooma doctor /data/vault      # while it is stopped
```

`nooma status /data/vault` works the same way. Both read the vault without changing the
database, beside a running serve too (ADR-0059). Doctor's `llm quality` check sends its fixed
set of 34 prompts to your provider, so each run is 34 paid calls on a cloud model.

**Never run them as root on the host path** (`sudo nooma doctor /srv/nooma/data/vault`). A
read-only open still creates `nooma.db-wal` and `nooma.db-shm` beside the database when they are
missing, owned by whoever runs it, and they stay. Owned by root, they can stop the container's
serve from opening the vault. If it happened, give them back before starting Nooma:

```sh
sudo chown 65532:65532 /srv/nooma/data/vault/nooma.db-wal /srv/nooma/data/vault/nooma.db-shm
```

### Building from source

`compose.build.yml` builds the working tree with `deploy/docker/Dockerfile` instead of pulling,
as `nooma:local`, so a source build never passes for a release. Add it to every command that
creates the container:

```sh
docker compose -f deploy/docker/compose.yml -f deploy/docker/compose.build.yml up -d --build
```

`.git` is part of the build context because the version stamp is `git describe`, as in the
Makefile. On a 1 GB droplet, if the build is killed for lack of memory (`signal: killed`), add
swap once and build again:

```sh
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile
sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

## 2. Access over Tailscale

```sh
sudo tailscale serve --bg 7777
```

This publishes `http://127.0.0.1:7777` at `https://<host>.<tailnet>.ts.net` to the tailnet only.
Log in to the UI with the `NOOMA_AUTH_TOKEN` value.

The DigitalOcean cloud firewall allows inbound 80 and 443 for the other apps, and nothing else;
SSH goes over Tailscale (`tailscale up --ssh`), so 22 stays closed. Port 7777 is never opened.

## 3. Daily encrypted backup

`nooma backup` writes a verified snapshot while serve runs (ADR-0049);
[`deploy/backup/nooma-backup.sh`](../deploy/backup/nooma-backup.sh) runs it in the container,
keeps the newest 14 locally, copies them through an rclone `crypt` remote and deletes remote
copies older than 30 days. Google only ever stores ciphertext.

Set up rclone on the host once (`sudo rclone config`, as root, who runs the cron):

1. `n`, name `gdrive`, storage `drive`, scope `drive.file`; authorize as `rclone config` says
   (on a headless host, `rclone authorize "drive"` on a machine with a browser).
2. `n`, name `nooma-crypt`, storage `crypt`, remote `gdrive:nooma-backups`, filename encryption
   `standard`, and let rclone generate both passwords.
3. **Store both passwords outside the droplet** (a password manager). Without them the backups
   cannot be read.

Then schedule it in root's crontab (`sudo crontab -e`):

```cron
30 4 * * * NOOMA_RCLONE_REMOTE=nooma-crypt: /opt/nooma/deploy/backup/nooma-backup.sh >> /var/log/nooma-backup.log 2>&1
```

04:30 local time, after the 03:00 consolidation. Each run logs one line ending `ok` or
`FAILED`. `NOOMA_COMPOSE_FILE`, `NOOMA_BACKUP_DIR`, `NOOMA_BACKUP_KEEP` and
`NOOMA_REMOTE_MAX_AGE` override the defaults.

## 4. Restore

A backup is `nooma-YYYYMMDD-HHMMSS.db` (UTC) and the matching `.yml`. Pass the one to restore
as the argument and paste the block whole: `-e` stops it at the first failing step, before the
live files are replaced. A name that matches no backup copies nothing, so the block checks both
files arrived before it stops Nooma.

```sh
sudo bash -e -s nooma-20261022-073000 <<'RESTORE'
b=$1
rclone copy nooma-crypt: /tmp/restore --include "$b.*"   # or cp from /srv/nooma/backups
test -f "/tmp/restore/$b.db"    # a wrong name stops here, before Nooma stops;
test -f "/tmp/restore/$b.yml"   # one line each: -e ignores a failure inside an && list
cd /opt/nooma
docker compose -f deploy/docker/compose.yml stop nooma
cd /srv/nooma/data/vault
mkdir before-restore
for f in nooma.db nooma.db-wal nooma.db-shm nooma.yml; do
  if [ -e "$f" ]; then mv "$f" before-restore/; fi
done
install -o 65532 -g 65532 -m 600 "/tmp/restore/$b.db" nooma.db
install -o 65532 -g 65532 -m 644 "/tmp/restore/$b.yml" nooma.yml
cd /opt/nooma
docker compose -f deploy/docker/compose.yml start nooma
RESTORE
```

The old database and config move aside, the database with its `-wal` and `-shm` if any: they
belong to it, and left beside the snapshot they would be replayed into it. Nothing is deleted.
An older snapshot is migrated forward when serve opens it.

**A second restore stops at `mkdir before-restore`**: the first one's folder is still there.
Nothing in the vault has moved, but Nooma is already stopped. Rename the old folder
(`sudo mv /srv/nooma/data/vault/before-restore /srv/nooma/data/vault/before-restore-1`), or
delete it once you no longer need it, and run the block again.

On a new host, do step 1 without `nooma init`, create the vault directory with
`sudo install -d -o 65532 -g 65532 /srv/nooma/data/vault`, and run the block.

## 5. Upgrade

Pull the release and recreate the container:

```sh
cd /opt/nooma && sudo git pull
docker compose -f deploy/docker/compose.yml pull
docker compose -f deploy/docker/compose.yml up -d
```

`pull_policy: always` makes every `up -d` check the registry too, so a skipped `pull` still
never runs a stale `latest`. The `git pull` brings the compose files and backup script of the
newest `main`; the binary is the image's. With `NOOMA_VERSION` pinned, set it to the new release first. To run unreleased
`main`, use the source build: `git pull`, then the `up -d --build` of
[Building from source](#building-from-source).

Migrations run when serve opens the vault. Take a backup first if the release notes mention a
migration.

### Upgrading from a source build

A host that ran Nooma before releases existed built `nooma:local` with `up -d --build`. That
command no longer builds: `compose.yml` has no `build:` section, so it only pulls, and before
the first release it fails with an error naming `ghcr.io/rengo/nooma` and `denied`
(`requested access to the resource is denied`) and leaves the old container running. GHCR
answers an anonymous pull the same way whether the image does not exist yet or its package is
private, so the error alone does not tell the two apart: the repository's Releases page does.
Switch in this order:

1. **Until the first release is published**, keep building from source after every
   `git pull`:

   ```sh
   docker compose -f deploy/docker/compose.yml -f deploy/docker/compose.build.yml up -d --build
   ```

2. **After the first `v*` tag**, check that its `release` workflow is green, then make the image
   pullable once. Either make the package public: on GitHub, the repository's **Packages** →
   `nooma` → **Package settings** → **Danger Zone** → **Change visibility** → **Public**. Or log
   the host in, with a classic token that has `read:packages`:
   `echo <token> | docker login ghcr.io -u <user> --password-stdin`.

3. **Then switch to pulling** (`docker compose -f deploy/docker/compose.yml pull`, then
   `docker compose -f deploy/docker/compose.yml up -d`). The container is recreated from
   the release, and the vault and backups are mounts, so nothing in them changes. From then
   on, upgrade as above. `nooma:local` can be removed with `docker image rm nooma:local`.

## 6. Export the whole brain

`nooma export` writes the vault into one `.noomabundle` file: the database, `nooma.yml` and the
attachments, with a manifest of checksums that is checked again before the file gets its name
([ADR-0057](adr/0057-a-bundle-is-a-checked-zip-of-the-vault.md)). It never carries `.env`, so the
secrets stay on this host. Like doctor, run it through the container, as the user serve runs as,
and write it into `/backups`, the mounted folder (`/srv/nooma/backups` on the host):

```sh
docker compose -f deploy/docker/compose.yml exec nooma nooma export /data/vault /backups/brain-20261029.noomabundle   # while Nooma runs
docker compose -f deploy/docker/compose.yml run --rm nooma export /data/vault /backups/brain-20261029.noomabundle     # while it is stopped
```

Never run it as root on the host path: it reads the vault the way doctor does, and the same
root-owned `-wal` and `-shm` files could stop serve from opening it
([Check the vault](#check-the-vault-doctor-and-status)).

The file belongs to uid 65532 and only it can read it, so copy it out with `sudo`, then remove
the copy left in `/backups`:

```sh
sudo cp /srv/nooma/backups/brain-20261029.noomabundle ~ && sudo chown "$USER" ~/brain-20261029.noomabundle
sudo rm /srv/nooma/backups/brain-20261029.noomabundle
```

`nooma.yml` travels byte for byte. Nooma refuses a key it does not know, so a pasted
`api_key: sk-…` stops the export instead of going in, and it refuses an endpoint whose URL carries
a user, a password or a query string. A **comment** is never read and goes in as written: do not
keep a secret in a comment.

An attachment whose name Windows cannot hold (a `:`, `?` or `*`, a trailing space or dot, a
device name such as `CON` or `aux.txt`) makes the export fail, and the error names the file:
rename it and export again. A bundle has to open on every system.

An export that is interrupted (killed, or the host loses power) leaves its `.partial` file and
a working folder named `.nooma-export-…` in `/backups`. Neither is a bundle; delete both by hand
(`sudo rm -r /srv/nooma/backups/*.noomabundle.partial /srv/nooma/backups/.nooma-export-*`) and
export again.

The bundle is the whole brain, unencrypted. Move it over Tailscale (`scp` to a machine on the
tailnet), not through a shared folder. The daily backup job neither uploads nor prunes it: it
only handles files named `nooma-*.db` and `nooma-*.yml`.

## 7. Move a brain between machines

A brain moves as one bundle: `nooma export` on one machine, `nooma import` on the other
([ADR-0057](adr/0057-a-bundle-is-a-checked-zip-of-the-vault.md)). Import makes a **new** vault
and never writes over one, so the vault that receives a brain is always an empty directory.
The copy comes with **Telegram off**: two copies polling one bot token steal each other's
messages, so it stays off until you turn it on in the one copy that should answer. `.env`
never travels; each machine keeps its own keys.

**From the notebook to the server.** On the notebook, with nooma installed:

```sh
nooma export ~/brains/pablo.nooma ~/pablo.noomabundle
scp ~/pablo.noomabundle <host>:   # over Tailscale
```

On the server, hand the file to the container's user, stop Nooma, move the current vault aside
(nothing is deleted), import into the empty place, and start:

```sh
sudo install -o 65532 -g 65532 -m 600 ~/pablo.noomabundle /srv/nooma/backups/
cd /opt/nooma
docker compose -f deploy/docker/compose.yml stop nooma
sudo mv /srv/nooma/data/vault /srv/nooma/data/vault-before-import
docker compose -f deploy/docker/compose.yml run --rm nooma import /backups/pablo.noomabundle /data/vault
```

Import runs in the container, as uid 65532, so the new vault is owned as serve needs it. It
refuses before writing anything when the bundle is damaged, newer than this release (upgrade
first, section 5), or larger than the free space. An older bundle is migrated forward.

**Before starting, give the copy this server's settings.** The bundle's `nooma.yml` is the
notebook's: its `server:` block binds loopback with no token, so the server would listen on
nothing the tailnet reaches, and its `providers:` and `tasks:` are the notebook's. Open
`/srv/nooma/data/vault/nooma.yml` (`sudoedit`) and, from
`/srv/nooma/data/vault-before-import/nooma.yml`:

- **replace or add** its `server:`, `providers:` and `tasks:` blocks. A notebook vault with no
  providers configured has only the `# providers:` and `# tasks:` comments, so there is nothing
  to replace: add the server's blocks;
- under `channels.telegram`, take this server's `bot_token_env` and `allowed_chat_ids`, and leave
  `enabled: false` as imported until you turn it on below.

`vault-before-import/.env` does not come along, and in the container it need not: the secrets
reach serve from `deploy/docker/.env`. A server running nooma without Docker keeps its secrets
in the vault's `.env`, and must copy it by hand
(`sudo cp -p /srv/nooma/data/vault-before-import/.env /srv/nooma/data/vault/`).

Then start and check the bind:

```sh
docker compose -f deploy/docker/compose.yml start nooma
docker compose -f deploy/docker/compose.yml logs nooma | grep listening   # "nooma: listening on 0.0.0.0:7777"
```

A bind other than `0.0.0.0:7777`, or a refusal to start, means the `server:` block is still the
notebook's. To answer on Telegram from the server, make sure the notebook's copy has it off, then
set `enabled: true` under `channels.telegram` in `/srv/nooma/data/vault/nooma.yml` and
`docker compose -f deploy/docker/compose.yml restart nooma`. Keep `vault-before-import` until
the new vault has run for a while; then remove it or keep it, it is yours.

**From the server to the notebook.** Export on the server (section 6), copy the file out, and
on the notebook:

```sh
nooma import ~/pablo-20261029.noomabundle ~/brains/pablo-copy.nooma
```

The copy has Telegram off, and leave it so while the server answers. Its `nooma.yml` is the
server's: `server:` binds `0.0.0.0` and names the auth token, and `providers:` and `tasks:` are
the server's. Before `nooma serve`, set `server:` to the notebook's (`bind: 127.0.0.1`, or keep
the token and put it in `.env`), and `providers:`/`tasks:` to what the notebook uses. It has no
`.env`: add one beside its `nooma.yml` with the provider key if the copy should capture and
recall.

An import that is interrupted (killed, or the host loses power) leaves a hidden working folder
named `.nooma-import-…` beside the target, and no vault. Delete it by hand
(`sudo rm -r /srv/nooma/data/.nooma-import-*` on the server) and import again.
