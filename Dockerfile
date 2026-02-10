# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY services/api/go.mod services/api/go.sum ./services/api/

# Download dependencies
RUN cd pkg && go mod download
RUN cd services/api && go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY services/api/ ./services/api/

# Build
RUN cd services/api && CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/bin/api ./cmd/main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/bin/api .

# Expose port
EXPOSE 8080

# Run
CMD ["./api"]
