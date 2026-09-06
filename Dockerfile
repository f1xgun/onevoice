# syntax=docker/dockerfile:1.6

# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Disable Go workspace for Docker builds (workspace is for local dev only)
# Allow Go to auto-download required toolchain version
ENV GOWORK=off
ENV GOTOOLCHAIN=auto

# Copy only the module manifests first so the dependency-download layer
# stays cached when only Go source files change. The BuildKit cache mount
# on /go/pkg/mod persists the downloaded module archives across builds and
# across worktrees (it lives in the BuildKit builder, not the build ctx).
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY services/api/go.mod services/api/go.sum ./services/api/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd pkg && for i in 1 2 3; do go mod download && break || sleep 5; done

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd services/api && for i in 1 2 3; do go mod download && break || sleep 5; done

# Copy the actual source after deps are resolved.
COPY pkg/ ./pkg/
COPY services/api/ ./services/api/

# Build — reuse the module and build caches.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd services/api && CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/bin/api ./cmd/main.go

# Runtime stage
FROM alpine@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

RUN apk --no-cache add ca-certificates tzdata wget

RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/api .
COPY scripts/entrypoint-ulimit.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose port
EXPOSE 8080

USER 10001:10001

ENTRYPOINT ["/entrypoint.sh"]
CMD ["./api"]
