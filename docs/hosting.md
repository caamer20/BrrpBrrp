# Choose your own hosting setup

BrrpBrrp runs on your own computer or server. You decide whether it stays local, is reachable on your LAN, or is exposed through a domain, proxy, VPN or network configuration you manage. Installation has no dependency on a particular provider.

Linux supports native x64 and ARM64 packages. For containers, use the included `Dockerfile` and `compose.yaml`; the [Docker guide](docker.md) covers persistent storage, certificates and the separate UDP route for voice.

## Keep it local

Use the wizard's **This computer** option for loopback-only HTTP, or **My local network** for HTTPS on the server's private IPv4 address. LAN browsers need to trust the server certificate to use voice. No router port forwarding is needed.

## Use an HTTPS reverse proxy

Terminate HTTPS in the proxy of your choice and forward requests to BrrpBrrp. For a proxy running on the same computer, set these values in the settings file shown by the wizard:

```ini
BRRP_ADDR=127.0.0.1:8082
BRRP_BASE_URL=https://chat.example.com
BRRP_TLS_MODE=http-behind-proxy
BRRP_BEHIND_PROXY=true
BRRP_TRUSTED_PROXIES=127.0.0.1/32,::1/128
BRRP_ALLOW_INSECURE_HTTP=false
```

Replace the example hostname with yours. Configure the proxy's certificate and route to `http://127.0.0.1:8082`. Preserve the original host, forward WebSocket Upgrade/Connection headers for `/ws`, disable WebSocket buffering, allow long-lived connections, and set request-size limits above the app's attachment limit. See the [reverse-proxy example](deployment.md#reverse-proxy) for more details.

For a proxy on another machine, bind BrrpBrrp to the appropriate private interface, allow that proxy's IP in the host firewall and trust only its exact source CIDR. Avoid trusting all internet addresses. Restart BrrpBrrp after changing host settings.

## Terminate HTTPS directly in BrrpBrrp

Place your certificate chain and private key inside the data folder, set `BRRP_TLS_MODE=https`, set `BRRP_CERT_FILE` and `BRRP_KEY_FILE` to their relative paths, and choose the matching HTTPS base URL. You can configure the existing ACME certificate option instead if public TCP 443 reaches the app's TLS listener. See [HTTPS choices](security.md#https-choices) for the exact settings and renewal considerations.

You manage DNS and any router/firewall forwarding. The app does not automatically configure a domain, create public tunnels or alter router settings.

## Voice has its own network path

| Traffic | App port | Purpose |
| --- | --- | --- |
| HTTP/HTTPS | TCP 8082 by wizard default | Pages, chat, attachments and WebSocket signaling |
| Voice media | UDP 50000–50100 | WebRTC audio between clients and the server |
| Optional TURN | Configured relay ports: UDP by default, plus a TCP and a TLS fallback | Reachability where clients cannot use a direct media path |

An HTTP proxy alone does not carry voice media. When exposing the app through NAT, forward the configured UDP media range to the server and set `BRRP_VOICE_EXTERNAL_IP` to the reachable IP if needed. Permit the intended client networks in the server firewall. Use TURN where the network requires a relay, and review its separate listener and relay ranges in [security.md](security.md#firewall-and-voice). If some clients are on a network that blocks UDP, switch on the relay's TCP fallback — and the TLS one if the certificate is readable — in **Manage server → Settings** and open the matching TCP ports; both are off by default.

Test messaging, uploads and a two-way voice call from another network before inviting people. Keep automatic backups on a separate disk or host, and test restoring one. Public access is a separate deployment step from completing the local installation.
