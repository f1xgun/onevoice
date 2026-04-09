# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Disable Go workspace for Docker builds (workspace is for local dev only)
# Allow Go to auto-download required toolchain version
ENV GOWORK=off
ENV GOTOOLCHAIN=auto

# Copy everything (simpler and works with replace directives)
COPY pkg/ ./pkg/
COPY services/api/ ./services/api/

# Download dependencies
RUN cd pkg && go mod download
RUN cd services/api && go mod download

# Build
RUN cd services/api && CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/bin/api ./cmd/main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata wget

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/bin/api .

# Expose port
EXPOSE 8080

# Run
CMD ["./api"]
