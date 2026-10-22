# 08 — Running Nooma on a server

Nooma runs 24/7 on a Linux host in Docker, reachable only over Tailscale, with a daily
encrypted backup to Google Drive. The files are under [`deploy/`](../deploy); the backup
decision is [ADR-0049](adr/0049-backup-is-a-built-in-snapshot.md).

| On the host | What it is |
|---|---|
| `/opt/nooma` | the git clone; `deploy/docker/.env` holds the secrets (gitignored) |
| `/srv/nooma/data/vault` | the vault (`/data/vault` in the container) |
| `/srv/nooma/backups` | the local backups (`/backups` in the container) |

Nooma is never public. It has its own compose project and network, no `VIRTUAL_HOST`, is never
on the `proxy` network, and its port is published on `127.0.0.1` only. Telegram is long polling,
so no inbound port is needed.

## 1. Build and start

```sh
sudo git clone https://github.com/rengo/nooma /opt/nooma && cd /opt/nooma
sudo install -d -o 65532 -g 65532 -m 0700 /srv/nooma/data /srv/nooma/backups
sudo cp deploy/docker/.env.example deploy/docker/.env && sudo chmod 600 deploy/docker/.env
```

The image runs as uid 65532 (distroless `nonroot`), so both directories belong to it. The image
is built on the host from the clone: `.git` is part of the build context because the version
stamp is `git describe`, as in the Makefile.

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
docker compose -f deploy/docker/compose.yml build
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

### A 1 GB droplet

If the build is killed for lack of memory (`signal: killed`), add swap once and build again:

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

A backup is `nooma-YYYYMMDD-HHMMSS.db` (UTC) and the matching `.yml`.

```sh
cd /opt/nooma
sudo rclone copy nooma-crypt: /tmp/restore --include 'nooma-20261022-073000.*'   # or use /srv/nooma/backups
docker compose -f deploy/docker/compose.yml stop nooma
cd /srv/nooma/data/vault
sudo mkdir before-restore && sudo mv nooma.db nooma.db-wal nooma.db-shm nooma.yml before-restore/ 2>/dev/null; ls before-restore
sudo install -o 65532 -g 65532 -m 600 /tmp/restore/nooma-20261022-073000.db nooma.db
sudo install -o 65532 -g 65532 -m 644 /tmp/restore/nooma-20261022-073000.yml nooma.yml
cd /opt/nooma && docker compose -f deploy/docker/compose.yml start nooma
```

The old database and config move aside, the database with its `-wal` and `-shm` if any: they
belong to it, and left beside the snapshot they would be replayed into it. Nothing is deleted.
An older snapshot is migrated forward when serve opens it. On a new host, do step 1 without
`nooma init`, create the vault directory with
`sudo install -d -o 65532 -g 65532 /srv/nooma/data/vault`, and put the two files in it.

## 5. Upgrade

```sh
cd /opt/nooma && sudo git pull
docker compose -f deploy/docker/compose.yml up -d --build
```

Migrations run when serve opens the vault. Take a backup first if the release notes mention a
migration.
