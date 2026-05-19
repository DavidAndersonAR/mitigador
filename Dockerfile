# syntax=docker/dockerfile:1.7
#
# Multi-stage build for Mitigador.
#
# Targets:
#   mitigador  → final image with the daemon (default)
#   flowgen    → optional synthetic-flow generator (dev/lab only)
#
# This image is intended for evaluation, lab work, and Docker-Compose-based
# deployments. Production-grade installs on an ISP coletor box should use
# the .deb / .rpm packages produced by goreleaser + the systemd unit in
# deploy/systemd/ — Docker on the data-plane host adds an unnecessary
# attack surface.

# ─── Stage 1: build the Vue SPA ─────────────────────────────────────────
FROM node:20-alpine AS web-builder
RUN corepack enable && corepack prepare pnpm@9 --activate
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --prefer-offline
COPY web/ ./
# Override outDir so vite writes inside this stage's tree; the go-builder
# stage picks the result up by COPY --from=web-builder.
RUN pnpm exec vue-tsc --noEmit \
 && pnpm exec vite build --outDir /out/web_dist --emptyOutDir

# ─── Stage 2: build Go binaries ─────────────────────────────────────────
FROM golang:1.25-alpine AS go-builder
WORKDIR /src
RUN apk add --no-cache git
# Pull module cache first to leverage Docker layer caching.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download
COPY . .
# Replace whatever web_dist is checked-in with the freshly built SPA.
RUN rm -rf internal/api/web_dist
COPY --from=web-builder /out/web_dist/ internal/api/web_dist/
ARG VERSION=dev
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w -X github.com/mitigador/mitigador/internal/version.version=${VERSION}" \
        -o /out/mitigador ./cmd/mitigador \
 && go build -trimpath -ldflags="-s -w" -o /out/flowgen ./cmd/flowgen

# ─── Stage 3: mitigador runtime ─────────────────────────────────────────
FROM alpine:3.20 AS mitigador
RUN apk add --no-cache ca-certificates tini tzdata \
 && addgroup -S -g 10001 mitigador \
 && adduser  -S -u 10001 -G mitigador -h /var/lib/mitigador mitigador \
 && mkdir -p /etc/mitigador /var/lib/mitigador \
 && chown -R mitigador:mitigador /etc/mitigador /var/lib/mitigador
COPY --from=go-builder /out/mitigador /usr/local/bin/mitigador
USER mitigador
WORKDIR /var/lib/mitigador
# 8080 → HTTP API + SPA   |   2055/4739/6343 UDP → NetFlow / IPFIX / sFlow
EXPOSE 8080
EXPOSE 2055/udp 4739/udp 6343/udp
ENTRYPOINT ["/sbin/tini","--","/usr/local/bin/mitigador"]
CMD ["serve","--config","/etc/mitigador/config.yaml"]

# ─── Stage 4: flowgen runtime (dev only) ────────────────────────────────
FROM alpine:3.20 AS flowgen
RUN apk add --no-cache ca-certificates \
 && addgroup -S -g 10001 flowgen \
 && adduser  -S -u 10001 -G flowgen flowgen
COPY --from=go-builder /out/flowgen /usr/local/bin/flowgen
USER flowgen
ENTRYPOINT ["/usr/local/bin/flowgen"]
