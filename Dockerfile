# ── ocode Docker image ─────────────────────────────────────────────────────
# Multi-stage build: frontend assets → Go binary → minimal runtime image.
#
# Build args:
#   GO_VERSION       Go toolchain version (default: 1.26)
#   NODE_VERSION     Node version for the web UI build (default: 22, same as Dockerfile.cross)
#   ALPINE_VERSION   Runtime base image tag (default: 3.20)

ARG GO_VERSION=1.26
ARG NODE_VERSION=22
ARG ALPINE_VERSION=3.20

# ── Stage 1: web UI ───────────────────────────────────────────────────────
# web/ is a pnpm project (packageManager pinned in package.json); corepack
# provides that exact pnpm and --frozen-lockfile installs from pnpm-lock.yaml.
FROM node:${NODE_VERSION}-alpine AS web-builder

WORKDIR /build/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# ── Stage 2: Go binary ────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /build

# Cache module downloads separately
COPY go.mod go.sum ./
RUN go mod download

# Copy everything and build
COPY . .

# Built web UI (required for embedded assets)
COPY --from=web-builder /build/web/dist ./web/dist

# Build the static binary
RUN go build -ldflags="-s -w" -o /usr/local/bin/ocode .

# ── Stage 3: minimal runtime ──────────────────────────────────────────────
FROM alpine:${ALPINE_VERSION}

# Install runtime deps: ca-certificates for HTTPS, git for version control
# (ocode uses git via the TUI and subprocess commands)
RUN apk add --no-cache ca-certificates git bash

# Create a non-root user with a fixed HOME so all home-relative config paths
# (~/.config/opencode/, ~/.local/share/opencode/) resolve consistently.
RUN adduser -D -h /home/ocode ocode

COPY --from=builder /usr/local/bin/ocode /usr/local/bin/ocode

USER ocode
WORKDIR /workspace

# TUI needs a $HOME that matches our volume mounts
ENV HOME=/home/ocode

# Docker Compose uses ocode as the default entrypoint (TUI mode).
# Override with "serve" for the web server, or "run" for headless.
ENTRYPOINT ["ocode"]
CMD []
