#!/bin/sh
set -eu
# Restore only into a new data directory containing the backup ZIP.
exec "${BRRP_BINARY:-./brrpbrrp}" restore "$@"
