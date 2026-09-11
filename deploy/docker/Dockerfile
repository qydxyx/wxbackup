# syntax=docker/dockerfile:1
#
# Build from the repository root:
#   docker build -f deploy/docker/Dockerfile -t wxbackup .
#
# Requires cmd/server (Go 1.22). modernc.org/sqlite is pure Go, so CGO is off.

FROM golang:1.22-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

ARG VERSION=dev
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS="-trimpath"

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w -X main.Version=${VERSION}" -o /out/wxbackup ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -H -u 65532 wxbackup \
    && mkdir -p /data \
    && chown wxbackup:wxbackup /data

COPY --from=build /out/wxbackup /usr/local/bin/wxbackup

USER wxbackup
ENV WXBACKUP_PORT=20365 \
    WXBACKUP_DATA=/data

VOLUME ["/data"]
# App HTTP. Discovery/transfer ports are host-networked, not published here.
EXPOSE 20365

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${WXBACKUP_PORT}/health" || exit 1

ENTRYPOINT ["/usr/local/bin/wxbackup"]
