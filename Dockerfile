# PiWorker Production Dockerfile
# Multi-stage build: Bun (frontend) → Go (backend + embed) → Runtime

# =============================================================================
# Stage 1: Build Frontend with Bun
# =============================================================================
# Bun builds the static UI on the builder platform, including for ARMv7 targets.
# Legacy builders can pass BUILDPLATFORM explicitly (the Makefile does this).
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM oven/bun:1.3.14-alpine AS frontend-builder

WORKDIR /app/web

# Copy package files first for better caching
COPY web/package.json web/bun.lock ./

# Install dependencies
RUN bun install --frozen-lockfile

# Copy frontend source
COPY web/ ./

# Build production bundle
RUN bun run build

# =============================================================================
# Stage 2: Build Backend with Embedded Frontend
# =============================================================================
FROM golang:1.27.3-alpine AS backend-builder
ARG VERSION=dev

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy built frontend into embed location
COPY --from=frontend-builder /app/web/dist ./internal/webui/dist

# Build the binary
# CGO is needed for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION}" \
    -o piworker .

# =============================================================================
# Stage 3: Production Runtime
# =============================================================================
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 piworker && \
    adduser -u 1000 -G piworker -D -h /app piworker

WORKDIR /app

# Copy binary from builder
COPY --from=backend-builder /app/piworker .

# Create data directory
RUN mkdir -p /app/data && chown -R piworker:piworker /app

# Switch to non-root user
USER piworker

# HTTPS (local CA kept in the data volume under tls/) and the plain HTTP
# listener that redirects to it and serves /piworker-ca.crt and webhooks.
# Publish both unchanged; add the host's name or address to
# PIWORKER_TLS_HOSTS so the certificate covers it.
EXPOSE 8443 8080

# Health check: a liveness probe over loopback, so the certificate is not
# verified here. Follows PIWORKER_ADDR and PIWORKER_TLS_MODE.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD port="${PIWORKER_ADDR:-:8443}"; port="${port##*:}"; \
        if [ "${PIWORKER_TLS_MODE:-auto}" = off ]; then url="http://127.0.0.1:$port/api/health"; \
        else url="https://127.0.0.1:$port/api/health"; fi; \
        wget -q --spider --no-check-certificate "$url" || exit 1

# Default command
ENTRYPOINT ["./piworker"]
CMD ["-db", "/app/data/piworker.db"]
