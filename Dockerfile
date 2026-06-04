# IMAgent Relay — Dockerized
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o relay ./cmd/relay/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/relay /usr/local/bin/imagent-relay
RUN mkdir -p /var/www/html
EXPOSE 8099
ENTRYPOINT ["/usr/local/bin/imagent-relay", "-port", "8099"]
