#!/bin/sh
set -eu
agent_file="$HOME/Library/LaunchAgents/co.brrpbrrp.server.plist"
launchctl bootout "gui/$(id -u)/co.brrpbrrp.server" || true
rm -f "$agent_file"
printf '%s\n' 'Launch agent removed. Application, settings and data are preserved.'
