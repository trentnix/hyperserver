#!/usr/bin/env bash
# Operates only on the disposable rehearsal deployment. Never run against production.
set -euo pipefail
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
  [[ ! -e "state/backups/$name.db" && ! -e "state/backups/$name.env" && ! -e "state/backups/$name.complete" ]] || fail 'That backup name already exists. Choose another name.'
else
  [[ -s "state/backups/$name.db" && -s "state/backups/$name.env" && -f "state/backups/$name.complete" ]] || fail 'The backup is missing or incomplete.'
fi
"${compose[@]}" stop app
# Keep the app stopped on any failure. The operator decides when to restart.
if [[ "$action" == backup ]]; then
  "${compose[@]}" run --rm --no-deps maintenance sqlite3 /data/hyperserver.db ".backup '/backups/$name.db'"
  cp state/app.env "state/backups/$name.env"
  chmod 600 "state/backups/$name.env" "state/backups/$name.db"
  result=$("${compose[@]}" run --rm --no-deps maintenance sqlite3 "/backups/$name.db" 'PRAGMA integrity_check; PRAGMA foreign_key_check;')
  [[ "$result" == ok ]] || fail 'Backup integrity check failed. The app remains stopped.'
  touch "state/backups/$name.complete"
else
  result=$("${compose[@]}" run --rm --no-deps maintenance sqlite3 "/backups/$name.db" 'PRAGMA integrity_check; PRAGMA foreign_key_check;')
  [[ "$result" == ok ]] || fail 'Backup integrity check failed. The app remains stopped.'
  "${compose[@]}" run --rm --no-deps maintenance sqlite3 /data/hyperserver.db ".restore '/backups/$name.db'"
  cp "state/backups/$name.env" state/app.env
  chmod 600 state/app.env
fi
printf '%s complete. The app remains stopped. See docs/deployment.md for the restart command.\n' "$action"
