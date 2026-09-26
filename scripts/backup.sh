#!/bin/sh
set -eu
# Stop the server first. Arguments are passed through without shell evaluation.
exec "${BRRP_BINARY:-./brrpbrrp}" backup "$@"
