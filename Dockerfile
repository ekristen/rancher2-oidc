# syntax=docker/dockerfile:1.20-labs@sha256:dbcde2ebc4abc8bb5c3c499b9c9a6876842bf5da243951cd2697f921a7aeb6a9
FROM alpine:3.19 AS base
RUN apk add --no-cache ca-certificates
RUN addgroup -g 1000 oidc && \
    adduser -D -h /app -u 1000 -G oidc oidc
RUN mkdir -p /app/data && chown -R oidc:oidc /app/data
ENTRYPOINT ["/app/oidc-aggregator"]
CMD ["aggregator", "--port", "8080"]


# Build stage
FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache make git gcc libc-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN \
    --mount=type=cache,target=/go/pkg \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o /bin/oidc-aggregator .

FROM base AS goreleaser
ARG PROJECT_NAME=go-project-template
COPY --from=builder /bin/oidc-aggregator /app/
WORKDIR /app
USER oidc


FROM base
ARG PROJECT_NAME=go-project-template
COPY --from=builder /bin/oidc-aggregator /app/
WORKDIR /app
USER oidc
