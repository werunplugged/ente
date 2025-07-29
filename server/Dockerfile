FROM golang:1.23.4-alpine3.21 AS builder
RUN apk add --no-cache gcc musl-dev git build-base pkgconfig libsodium-dev

ENV GOOS=linux

WORKDIR /etc/ente/

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
  go build -gcflags "all=-N -l" -o museum cmd/museum/main.go
 # Install Delve
RUN go install github.com/go-delve/delve/cmd/dlv@latest

FROM alpine:3.21
COPY --from=builder /usr/lib/libsodium.so* /usr/lib
COPY --from=builder /etc/ente/museum .
COPY configurations configurations
COPY migrations migrations
COPY mail-templates mail-templates
COPY web-templates web-templates
COPY --from=builder /go/bin/dlv /

ARG GIT_COMMIT
ENV GIT_COMMIT=$GIT_COMMIT

CMD ["/dlv", "--listen=:40000", "--headless=true", "--api-version=2", "exec", "./museum"]