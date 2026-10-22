#!/usr/bin/env bash
# Daily Nooma backup (docs/08-deploy.md): a verified snapshot inside the
# container, then an encrypted copy to Google Drive through an rclone crypt
# remote. Run by root's cron or a systemd timer, for example:
#
#   30 4 * * * NOOMA_RCLONE_REMOTE=nooma-crypt: /opt/nooma/deploy/backup/nooma-backup.sh >> /var/log/nooma-backup.log 2>&1
#
# Exits non-zero when any step fails.
set -euo pipefail

remote="${NOOMA_RCLONE_REMOTE:-}"
compose_file="${NOOMA_COMPOSE_FILE:-/opt/nooma/deploy/docker/compose.yml}"
backup_dir="${NOOMA_BACKUP_DIR:-/srv/nooma/backups}"
keep="${NOOMA_BACKUP_KEEP:-14}"
remote_max_age="${NOOMA_REMOTE_MAX_AGE:-30d}"

log() { printf '%s nooma-backup: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
trap 'log "FAILED (exit $?)"' ERR

if [[ -z "$remote" ]]; then
  log "FAILED: set NOOMA_RCLONE_REMOTE to the rclone crypt remote, for example nooma-crypt:"
  exit 2
fi

# /data/vault and /backups are the container's side of compose.yml's mounts.
docker compose -f "$compose_file" exec -T nooma nooma backup /data/vault /backups --keep "$keep"

filters=(--include 'nooma-*.db' --include 'nooma-*.yml')
rclone copy "$backup_dir" "$remote" "${filters[@]}"
rclone delete "$remote" --min-age "$remote_max_age" "${filters[@]}"

log "ok: $backup_dir copied to $remote, remote copies older than $remote_max_age deleted"
