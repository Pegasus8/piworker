# PiWorker Production Dockerfile
# Multi-stage build: Node.js (frontend) → Go (backend + embed) → Runtime

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
FROM golang:1.25-alpine AS backend-builder

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
    -ldflags="-w -s -X main.version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
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

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/health || exit 1

# Default command
ENTRYPOINT ["./piworker"]
CMD ["-addr", ":8080", "-db", "/app/data/piworker.db"]
