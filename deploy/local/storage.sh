#!/usr/bin/env bash
# Operates only on the disposable rehearsal deployment. Never run against production.
set -euo pipefail
umask 077
cd "$(dirname "$0")"
action=${1:?Use backup or restore}
name=${2:?Supply a backup name}
fail() { printf '%s\n' "$*" >&2; exit 1; }
[[ "$action" == backup || "$action" == restore ]] || fail 'Use backup or restore.'
[[ "$name" =~ ^[a-zA-Z0-9_-]+$ ]] || fail 'Backup names must contain only letters, digits, underscores, and hyphens.'
[[ -d state/data && -f .env ]] || fail 'Run init.sh before maintenance.'
exec 9>state/maintenance.lock
flock -n 9 || { echo 'Another maintenance operation is running.' >&2; exit 1; }
unset COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES
compose=(docker --context default compose --file compose.yaml --env-file .env --project-name hyperserver-rehearsal)
if [[ "$action" == backup ]]; then
  [[ ! -e "state/backups/$name.db" && ! -e "state/backups/$name.blog.db" && ! -e "state/backups/$name.uploads.tar" && ! -e "state/backups/$name.env" && ! -e "state/backups/$name.complete" ]] || fail 'That backup name already exists. Choose another name.'
else
  [[ -s "state/backups/$name.db" && -s "state/backups/$name.blog.db" && -s "state/backups/$name.uploads.tar" && -s "state/backups/$name.env" && -f "state/backups/$name.complete" ]] || fail 'The backup is missing or incomplete.'
fi
"${compose[@]}" stop app
# Keep the app stopped on any failure. The operator decides when to restart.
if [[ "$action" == backup ]]; then
  "${compose[@]}" run --rm --no-deps maintenance sqlite3 /data/hyperserver.db ".backup '/backups/$name.db'"
  "${compose[@]}" run --rm --no-deps maintenance sqlite3 /data/blog.db ".backup '/backups/$name.blog.db'"
  tar -C state/data -cf "state/backups/$name.uploads.tar" uploads
  cp state/app.env "state/backups/$name.env"
  chmod 600 "state/backups/$name.env" "state/backups/$name.db" "state/backups/$name.blog.db" "state/backups/$name.uploads.tar"
  result=$("${compose[@]}" run --rm --no-deps maintenance sqlite3 "/backups/$name.db" 'PRAGMA integrity_check; PRAGMA foreign_key_check;')
  [[ "$result" == ok ]] || fail 'Backup integrity check failed. The app remains stopped.'
  result=$("${compose[@]}" run --rm --no-deps maintenance sqlite3 "/backups/$name.blog.db" 'PRAGMA integrity_check; PRAGMA foreign_key_check;')
  [[ "$result" == ok ]] || fail 'Blog backup integrity check failed. The app remains stopped.'
  tar -tf "state/backups/$name.uploads.tar" >/dev/null
  touch "state/backups/$name.complete"
else
  result=$("${compose[@]}" run --rm --no-deps maintenance sqlite3 "/backups/$name.db" 'PRAGMA integrity_check; PRAGMA foreign_key_check;')
  [[ "$result" == ok ]] || fail 'Backup integrity check failed. The app remains stopped.'
  result=$("${compose[@]}" run --rm --no-deps maintenance sqlite3 "/backups/$name.blog.db" 'PRAGMA integrity_check; PRAGMA foreign_key_check;')
  [[ "$result" == ok ]] || fail 'Blog backup integrity check failed. The app remains stopped.'
  # Stage and validate uploads before replacing either database.
  # Extract only a locally generated, trusted archive.
  restore_dir=$(mktemp -d state/data/restore-XXXXXXXX)
  tar -C "$restore_dir" -xf "state/backups/$name.uploads.tar"
  [[ -d "$restore_dir/uploads" ]] || fail 'Uploads archive is incomplete. The app remains stopped.'
  "${compose[@]}" run --rm --no-deps maintenance sqlite3 /data/hyperserver.db ".restore '/backups/$name.db'"
  "${compose[@]}" run --rm --no-deps maintenance sqlite3 /data/blog.db ".restore '/backups/$name.blog.db'"
  # Keep existing uploads recoverable, but allow recovery after their loss.
  if [[ -e state/data/uploads ]]; then
    mv state/data/uploads "$restore_dir/previous-uploads"
  fi
  mv "$restore_dir/uploads" state/data/uploads
  cp "state/backups/$name.env" state/app.env
  chmod 600 state/app.env
fi
printf '%s complete. The app remains stopped. See docs/deployment.md for the restart command.\n' "$action"
