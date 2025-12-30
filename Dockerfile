# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Install required system packages
RUN apk add --no-cache make git gcc libc-dev

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o /bin/oidc-aggregator .

# Final stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata sqlite

# Create non-root user
RUN addgroup -g 1000 oidc && \
    adduser -D -h /app -u 1000 -G oidc oidc

WORKDIR /app
USER oidc

# Copy binary from builder
COPY --from=builder /bin/oidc-aggregator /app/

# Create data directory with correct permissions
RUN mkdir -p /app/data && chown -R oidc:oidc /app/data

# Default port
EXPOSE 8080

ENTRYPOINT ["/app/oidc-aggregator"]
CMD ["aggregator", "--port", "8080"]
