# syntax=docker/dockerfile:1.20-labs@sha256:dbcde2ebc4abc8bb5c3c499b9c9a6876842bf5da243951cd2697f921a7aeb6a9
FROM alpine:3.19 AS base
RUN apk add --no-cache ca-certificates
RUN addgroup -g 1000 oidc && \
    adduser -D -h /app -u 1000 -G oidc oidc
RUN mkdir -p /app/data && chown -R oidc:oidc /app/data
ENTRYPOINT ["/app/rancher2-oidc"]
CMD ["aggregator", "--port", "8080"]

FROM golang:1.25-alpine AS build
COPY / /src
WORKDIR /src
RUN go build -a -ldflags '-linkmode external -extldflags "-static"' -o /bin/rancher2-oidc .

FROM base AS goreleaser
COPY rancher2-oidc /app/
WORKDIR /app
USER oidc

FROM base
COPY --from=build /bin/rancher2-oidc /app/
WORKDIR /app
USER oidc
