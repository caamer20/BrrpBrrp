# syntax=docker/dockerfile:1
ARG NODE_IMAGE=node:24.21.0-alpine3.24
ARG GO_IMAGE=golang:1.27.2-alpine3.24

FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

# Notices are generated where their inputs actually exist. The generator reads the
# module set the compiler links (a Go toolchain and a populated module cache), the
# packages bundled into the served web build (web/package.json and the licenses in
# web/node_modules), and the standard library. This stage therefore needs both
# runtimes; the Go builder stage below deliberately does not run Node at all.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS notices
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go version && go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/*.go ./web/
# The served bundle is embedded by the Go package the generator inspects, so the
# embed pattern must resolve here too.
COPY --from=frontend /src/web/dist/ ./web/dist/
COPY web/package.json ./web/package.json
COPY --from=frontend /src/web/node_modules ./web/node_modules
COPY scripts/licenses.mjs ./scripts/licenses.mjs
COPY scripts/release-targets.json ./scripts/release-targets.json
RUN --mount=type=cache,target=/go/pkg/mod apk add --no-cache nodejs && \
    mkdir -p /notices && \
    BRRP_GO=go node scripts/licenses.mjs ./cmd/brrpbrrp /notices/THIRD_PARTY_NOTICES.txt

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go version && go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/*.go ./web/
COPY --from=frontend /src/web/dist/ ./web/dist/
ARG TARGETOS
ARG TARGETARCH
RUN mkdir -p /out/data /out/tmp && chown 10001:10001 /out/data && chmod 1777 /out/tmp
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/brrpbrrp ./cmd/brrpbrrp

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/brrpbrrp /usr/local/bin/brrpbrrp
# A redistributed image must carry both the project's own license and the notices
# for everything linked into the binary and bundled into the web build.
COPY LICENSE /usr/share/licenses/brrpbrrp/LICENSE
COPY --from=notices /notices/THIRD_PARTY_NOTICES.txt /usr/share/licenses/brrpbrrp/THIRD_PARTY_NOTICES.txt
COPY --from=build --chown=10001:10001 /out/data/ /data/
COPY --from=build /out/tmp/ /tmp/
USER 10001:10001
WORKDIR /data
ENV BRRP_DATA_DIR=/data \
    BRRP_ADDR=:8082 \
    BRRP_BASE_URL=https://localhost:8082 \
    BRRP_TLS_MODE=auto
VOLUME ["/data"]
EXPOSE 8082/tcp 50000-50100/udp
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/brrpbrrp", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/brrpbrrp"]
CMD ["serve"]
