# Host BrrpBrrp with Docker

Use the same BrrpBrrp server in a container on a Linux x64 or ARM64 host. The image builds from this source folder or the versioned Docker source ZIP and includes the latest compiled interface. A Docker Engine with the Compose and Buildx plugins is required. No image has been published to a registry; the commands below build `brrpbrrp:local` on your host.

The build has four stages: the frontend bundle, a **notices** stage that generates `THIRD_PARTY_NOTICES.txt` from the linked Go modules, the bundled frontend packages and the standard library (it needs both Node and the Go toolchain, so it is the only stage that has both), the Go build, and a `scratch` runtime image that carries the binary, the project `LICENSE` and those notices under `/usr/share/licenses/brrpbrrp/`. Because the notices depend on the dependency set, a build whose notices cannot be generated fails rather than publishing an image without them.

Before building, `node scripts/check-docker-inputs.mjs` audits the build context statically: every `COPY` source must exist and survive `.dockerignore`, any stage that generates notices must have both runtimes and all of its inputs, and the runtime stage must ship the license and notices. Its `--self-test` runs the same audit against the pre-fix Dockerfile shape and fails if the audit stops detecting the defect. This is a static check and does not replace a real image build; use `make docker-check` on a Docker host for the container lifecycle, persistence and offline-backup run.

## First start

From the source folder:

```sh
cp .env.docker.example .env.docker
```

Edit `.env.docker` for the address people will use. The default binds the web port to this computer and uses `https://localhost:8082`. For a LAN host at **192.168.1.25**, for example:

```ini
BRRP_BASE_URL=https://192.168.1.25:8082
BRRP_BIND_IP=0.0.0.0
BRRP_WEB_PORT=8082
BRRP_TLS_MODE=auto
BRRP_VOICE_EXTERNAL_IP=192.168.1.25
```

Use your host's actual IP. Keep the voice port mappings unchanged unless you also change the app's configured range in `compose.yaml`. Then:

```sh
docker compose --env-file .env.docker up -d --build
docker compose --env-file .env.docker ps
docker compose --env-file .env.docker logs --tail 50 brrpbrrp
docker compose --env-file .env.docker exec brrpbrrp /usr/local/bin/brrpbrrp setup-token
```

With `auto` mode and no public certificate settings, the app generates a local certificate authority and a certificate for the configured hostname/IP. Export **only the public root** and trust it on each client that will use voice or install the app:

```sh
docker compose --env-file .env.docker cp brrpbrrp:/data/certs/root.crt ./brrpbrrp-root.crt
```

See [certificate trust](security.md#https-choices) for context. Keep private keys in the data volume. Open your configured `BRRP_BASE_URL`, enter the setup token, and create the first administrator. Setup closes after that account exists.

The container runs `serve` directly. Docker manages its lifecycle; there is no service-installation wizard inside the container. Only one running container may use a given BrrpBrrp data volume.

## Storage and permissions

The named volume `brrpbrrp_brrpbrrp-data` mounts at `/data` under the default Compose project name. It contains SQLite, uploads, certificates, push configuration, generated previews and backups. It survives ordinary `docker compose down` and container replacement. **`docker compose down --volumes` deletes that volume and its data.** Changing the Compose project name selects a different volume.

The image runs as UID/GID **10001:10001**. A fresh named volume inherits the image's writable data-directory ownership. If you choose a bind mount instead, create a dedicated directory first and give UID/GID 10001 access to it. Do not mount a running native installation's live database into a second server; use an offline copy or a verified backup/restore.

The root filesystem is read-only in Compose. `/data` is writable, and `/tmp` is a disposable 64 MiB memory filesystem. Logs go to Docker, with rotation configured. Node.js and Go are build dependencies; the runtime image contains the compiled server and CA certificates, with no shell or package manager.

For scheduled backup copies on a separate mounted destination, set `BRRP_BACKUP_HOST_DIR` to an absolute private host directory in `.env.docker`, give UID/GID 10001 write access, and add `-f compose.backup.yaml` after `-f compose.yaml` to every Compose command. Add `-f compose.turn.yaml` too when using TURN. The override maps the directory to `/backup-copy` and sets `BRRP_BACKUP_COPY_DIR`. Enable scheduled backups in **Manage server → Maintenance** (the default cadence is 12 hours, the interval the recovery objective of at most 24 hours of loss assumes) and replicate/protect the host destination off machine. A bind mount alone does not provide off-host recovery.

## Public HTTPS

Choose one of these approaches.

### Existing HTTPS reverse proxy

Keep the web binding private, route the proxy to the mapped TCP port, and set:

```ini
BRRP_BASE_URL=https://chat.example.com
BRRP_BIND_IP=127.0.0.1
BRRP_WEB_PORT=8082
BRRP_TLS_MODE=http-behind-proxy
BRRP_BEHIND_PROXY=true
BRRP_TRUSTED_PROXIES=172.18.0.1/32
BRRP_VOICE_EXTERNAL_IP=YOUR_REACHABLE_IPV4
```

**The proxy CIDR above is an example, not a universal Docker address.** Trust the exact source address that the proxy uses to reach the container. With a host-side proxy through a published bridge port, that is commonly the Docker bridge gateway. Inspect the actual gateway:

```sh
docker inspect --format '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' \
  "$(docker compose --env-file .env.docker ps -q brrpbrrp)"
```

For a proxy in another container, connect it to the app's Docker network, route to `http://brrpbrrp:8082`, and trust its specific source IP/CIDR. Container `127.0.0.1` refers to that container. See [reverse-proxy settings](deployment.md#reverse-proxy) for forwarded headers, `/ws` upgrades and upload limits. Recreate the service after changing `.env.docker`.

### Direct certificate or ACME

For an existing certificate, place its chain and key inside `/data`, ensure UID 10001 can read them, set `BRRP_CERT_FILE` / `BRRP_KEY_FILE` to their relative paths, and choose `BRRP_TLS_MODE=https`. Restart the container after renewing externally managed certificate files.

For direct ACME HTTPS, start with a fresh volume and configure your public hostname, `BRRP_TLS_MODE=https`, `BRRP_DOMAIN`, `BRRP_ACME_EMAIL`, `BRRP_BIND_IP=0.0.0.0` and `BRRP_WEB_PORT=443`. Public TCP 443 must reach the container's TCP 8082 listener, and DNS must point to the host. Existing local certificate files take precedence over ACME; use the existing proxy option when converting a previously configured local deployment unless you deliberately migrate its certificate configuration. See [HTTPS choices](security.md#https-choices).

## Voice and TURN

Chat, pages and signaling use the web TCP port. Voice uses separate UDP ports:

| Traffic | Container ports | Host mappings |
| --- | --- | --- |
| Web/HTTPS and WebSocket signaling | TCP 8082 | `BRRP_WEB_PORT`, default 8082 |
| WebRTC audio | UDP 50000–50100 | Same range; included in Compose |
| Optional TURN listener | UDP 3478 by default | Same port; optional Compose file |
| Optional TURN relay | UDP 49152–49999 | Same range; optional Compose file |

Set `BRRP_VOICE_EXTERNAL_IP` to the host IP clients can reach. Through Docker bridge networking, advertising the container's private IP is usually insufficient. For internet access, forward UDP 50000–50100 through the router/firewall to the Docker host and retain the same port numbers. `BRRP_MEDIA_BIND_IP` controls the host interface for UDP mappings. A web-only HTTP tunnel or reverse proxy does not relay WebRTC audio.

If a restrictive network needs the built-in TURN relay, also publish its ports:

```sh
docker compose --env-file .env.docker -f compose.yaml -f compose.turn.yaml up -d --build
```

Then enable TURN in **Manage server → Settings**, set its reachable IPv4 address and matching port, and forward the listener and relay range at the firewall/router. `BRRP_TURN_PORT` controls the Docker mapping; it does not enable or configure TURN in the application. Continue using both Compose files for subsequent lifecycle commands. The built-in TURN service supports UDP only.

On a Linux host, host networking is another option when avoiding bridge NAT or large port mappings matters. Configure `network_mode: host`, remove the `ports` entries, and choose `BRRP_ADDR` for the host listener. This exposes configured listeners directly on the host; adjust its firewall accordingly. The supplied Compose file uses bridge networking. Validate a two-way voice call from another network before inviting users.

## Updates and backups

Use **Manage server → Maintenance → Create backup** while the app is running, then keep the downloaded ZIP on separate storage. For an offline backup:

```sh
docker compose --env-file .env.docker stop brrpbrrp
docker compose --env-file .env.docker run --rm --no-deps brrpbrrp backup --out backups/manual.zip
docker compose --env-file .env.docker cp brrpbrrp:/data/backups/manual.zip ./brrpbrrp-backup.zip
docker compose --env-file .env.docker up -d
```

The offline command needs the live container stopped so it can acquire the data lock. The ZIP contains application data and uploads, but not host configuration or TLS files; back up `.env.docker` and the certificate directory separately and privately. Follow [restore instructions](admin.md) using a fresh data volume/folder. Copy the ZIP into that fresh volume, then run the image's `restore --file /data/restore.zip` command with that volume mounted at `/data`. Keep the original volume until the restored installation has been verified.

To update from changed source, first export a backup and retain the current image under a local rollback tag. Then:

```sh
docker tag brrpbrrp:local brrpbrrp:previous
docker compose --env-file .env.docker build --pull
docker compose --env-file .env.docker up -d
docker compose --env-file .env.docker ps
```

Rebuilding replaces the container image while retaining its data volume. A restart briefly disconnects chat/voice. Browsers offer **Update now** when the embedded frontend changes. Keep the prior image until the new one has passed your checks. For rollback, set the service's `image` to the retained tag and recreate with `--no-build --pull never`; after any database schema upgrade, use the prior backup in a fresh volume with the matching older image.

## Build and verify

Build for the current host and run the disposable container smoke test:

```sh
docker build -t brrpbrrp:local .
node scripts/check-docker.mjs brrpbrrp:local
```

The test creates its own named volume and containers. It checks non-root startup, validated HTTPS, health, first-account setup, messages/uploads surviving container recreation, compressed assets, backup creation and graceful stop, then removes its own resources. Node 24 is required only for this developer test.

The native release helper also creates `dist/brrpbrrp-docker-source-<version>.zip` and `dist/DOCKER-SOURCE-SHA256SUMS`. Verify the checksum, extract the ZIP to a fresh folder, and run the same Docker build/Compose commands there. The source archive includes the Docker build inputs and public operator guides; it contains no local data directory, certificates, compiled app or dependency cache.

The Docker CI workflow builds and tests x64, then cross-builds ARM64. To export an image archive for a host without source/build tools:

```sh
docker buildx build --platform linux/amd64 --load -t brrpbrrp:local .
docker save -o brrpbrrp-linux-amd64-image.tar brrpbrrp:local
# On the destination: docker load -i brrpbrrp-linux-amd64-image.tar
```

Use `linux/arm64` for ARM64. Public registry publishing is a separate deployment step. The build accepts `NODE_IMAGE` and `GO_IMAGE` build arguments if an operator needs verified digest-pinned base images.

For health diagnostics:

```sh
docker compose --env-file .env.docker exec brrpbrrp /usr/local/bin/brrpbrrp healthcheck
```

Exit 0 means the configured listener returned a valid setup-status response and, for direct HTTPS, its certificate matched the configured hostname and trusted roots. It checks the app locally without taking the database lock. It does not establish external proxy/DNS reachability, voice quality or push delivery.

Once scheduled backups and the extra copy are active, run `production-check` in the same container to check the browser HTTPS address, certificate lifetime, backup status/freshness and free disk:

```sh
docker compose --env-file .env.docker -f compose.yaml -f compose.backup.yaml exec -T brrpbrrp /usr/local/bin/brrpbrrp production-check
```

It exits nonzero so a host scheduler or monitoring service can alert. Run an external reachability check from another network as well. See the [production runbook](production-readiness.md).

### Verification status

The Compose configurations, health probe and Linux cross-builds are validated locally as recorded in [acceptance.md](acceptance.md#docker-packaging--2026-09-14). This development Mac has no Docker engine, so image construction, volume ownership and container smoke results must still be confirmed by the supplied Docker CI job or a Docker-enabled host.

### References

The packaging follows Docker's documentation for [multi-stage builds](https://docs.docker.com/build/building/multi-stage/), [persistent volumes](https://docs.docker.com/engine/storage/volumes/), [Compose services](https://docs.docker.com/reference/compose-file/services/) and [host networking](https://docs.docker.com/engine/network/drivers/host/).
