# syntax=docker/dockerfile:1.4

FROM golang:1.24.1-alpine AS build-dev
WORKDIR /go/src/app
COPY --link go.mod go.sum ./
RUN apk add --no-cache upx || \
    go version && \
    go mod download
COPY --link . .
RUN CGO_ENABLED=0 go install -buildvcs=false -trimpath -ldflags '-w -s'
RUN [ -e /usr/bin/upx ] && upx /go/bin/nostr-status-lastfm || echo
FROM scratch
COPY --link --from=build-dev /go/bin/nostr-status-lastfm /go/bin/nostr-status-lastfm
COPY --from=build-dev /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
CMD ["/go/bin/nostr-status-lastfm"]
