#!/bin/sh
set -eu
package_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
binary=${1:-"$package_dir/brrpbrrp"}
if [ ! -f "$binary" ]; then echo 'Pass the path to the compiled brrpbrrp binary.' >&2; exit 1; fi
if launchctl print "gui/$(id -u)/co.brrpbrrp.server" >/dev/null 2>&1; then echo 'Unload the existing agent and back up before updating.' >&2; exit 1; fi
install_dir="$HOME/Library/Application Support/BrrpBrrp"
agent_file="$HOME/Library/LaunchAgents/co.brrpbrrp.server.plist"
mkdir -p "$install_dir/data" "$HOME/Library/LaunchAgents"
chmod 0700 "$install_dir" "$install_dir/data"
cp "$binary" "$install_dir/brrpbrrp"
chmod 0755 "$install_dir/brrpbrrp"
if [ ! -f "$install_dir/server.env" ]; then
  printf 'BRRP_DATA_DIR=%s/data\nBRRP_ADDR=:8080\nBRRP_BASE_URL=https://localhost:8080\nBRRP_TLS_MODE=auto\nBRRP_LOG_FILE=logs/server.log\n' "$install_dir" > "$install_dir/server.env"
  chmod 0600 "$install_dir/server.env"
fi
# plutil inserts paths as strings, including spaces and XML-sensitive characters.
plutil -create xml1 "$agent_file"
plutil -insert Label -string co.brrpbrrp.server "$agent_file"
plutil -insert ProgramArguments -json '[]' "$agent_file"
plutil -insert ProgramArguments.0 -string "$install_dir/brrpbrrp" "$agent_file"
plutil -insert ProgramArguments.1 -string serve "$agent_file"
plutil -insert ProgramArguments.2 -string --config "$agent_file"
plutil -insert ProgramArguments.3 -string "$install_dir/server.env" "$agent_file"
plutil -insert WorkingDirectory -string "$install_dir" "$agent_file"
plutil -insert RunAtLoad -bool true "$agent_file"
plutil -insert KeepAlive -json '{"SuccessfulExit":false}' "$agent_file"
plutil -insert ThrottleInterval -integer 5 "$agent_file"
plutil -insert ExitTimeOut -integer 25 "$agent_file"
plutil -lint "$agent_file"
printf 'Installed. Edit %s/server.env, then run:\nlaunchctl bootstrap gui/%s "%s"\n' "$install_dir" "$(id -u)" "$agent_file"
