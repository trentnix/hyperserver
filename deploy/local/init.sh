#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
umask 077
mkdir -p state/data state/backups state/tls
if [[ ! -e state/app.env ]]; then
  printf 'HYPERSERVER_HTTP_SESSION_JWTKEY=%s\nHYPERSERVER_AUTH_JWTKEY=%s\n' \
    "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" > state/app.env
fi
if [[ ! -e state/tls/cert.pem && ! -e state/tls/key.pem ]]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 30 \
    -keyout state/tls/key.pem -out state/tls/cert.pem \
    -subj /CN=localhost -addext subjectAltName=DNS:localhost
fi
[[ -s state/tls/cert.pem && -s state/tls/key.pem ]]
revision=$(git rev-parse HEAD)
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  revision="$revision-dirty"
fi
printf 'LOCAL_UID=%s\nLOCAL_GID=%s\nCOMMIT=%s\nVERSION=development\n' \
  "$(id -u)" "$(id -g)" "$revision" > .env
printf 'Prepared local rehearsal. No containers started.\n'
