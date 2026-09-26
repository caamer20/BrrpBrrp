#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then
  echo 'Run this installer with sudo.' >&2
  exit 1
fi
package_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$package_dir/brrpbrrp"}
if [ ! -f "$binary" ]; then echo 'Pass the path to the compiled brrpbrrp binary.' >&2; exit 1; fi
if systemctl is-active --quiet brrpbrrp; then echo 'Stop BrrpBrrp and back up before updating.' >&2; exit 1; fi
if ! id brrpbrrp >/dev/null 2>&1; then useradd --system --home-dir /var/lib/brrpbrrp --shell /usr/sbin/nologin brrpbrrp; fi
install -m 0755 "$binary" /usr/local/bin/brrpbrrp
install -d -o brrpbrrp -g brrpbrrp -m 0700 /var/lib/brrpbrrp
install -d -m 0750 -o root -g brrpbrrp /etc/brrpbrrp
if [ ! -f /etc/brrpbrrp/server.env ]; then
  cat > /etc/brrpbrrp/server.env <<'CONFIG'
BRRP_DATA_DIR=/var/lib/brrpbrrp
BRRP_ADDR=:8080
BRRP_BASE_URL=https://localhost:8080
BRRP_TLS_MODE=auto
BRRP_LOG_LEVEL=info
CONFIG
  chown root:brrpbrrp /etc/brrpbrrp/server.env
  chmod 0640 /etc/brrpbrrp/server.env
fi
install -m 0644 "$package_dir/scripts/linux/brrpbrrp.service" /etc/systemd/system/brrpbrrp.service
systemctl daemon-reload
systemctl enable brrpbrrp
printf '%s\n' 'Installed. Edit /etc/brrpbrrp/server.env for your public URL, then run: sudo systemctl start brrpbrrp'
