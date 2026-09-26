#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then echo 'Run with sudo.' >&2; exit 1; fi
systemctl disable --now brrpbrrp
rm -f /etc/systemd/system/brrpbrrp.service
systemctl daemon-reload
printf '%s\n' 'Service removed. Binary, settings, service account and all data have been preserved.'
