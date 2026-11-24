#!/usr/bin/env sh
set -eu

: "${LITESTREAM_CONFIG:?set LITESTREAM_CONFIG to the Litestream config path}"
: "${API_DB_PATH:?set API_DB_PATH to the target SQLite database path}"

systemctl stop go-sqlite-api
mkdir -p "$(dirname "$API_DB_PATH")"
rm -f "$API_DB_PATH" "$API_DB_PATH-wal" "$API_DB_PATH-shm"
litestream restore -config "$LITESTREAM_CONFIG" -o "$API_DB_PATH" "$API_DB_PATH"
systemctl start go-sqlite-api
