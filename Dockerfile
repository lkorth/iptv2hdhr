# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/iptv2hdhr ./cmd/iptv2hdhr

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata ffmpeg
WORKDIR /app
COPY --from=build /out/iptv2hdhr /app/iptv2hdhr

# Config lives in a mounted volume so it persists across image upgrades.
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK CMD wget -q -O- http://localhost:8080/lineup_status.json || exit 1

ENTRYPOINT ["/app/iptv2hdhr", "-config", "/data/config.yaml"]
