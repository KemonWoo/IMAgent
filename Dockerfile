# ── IMAgent Relay Dockerfile ──
# 多阶段构建，最终镜像 < 10MB
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o relay ./cmd/relay/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /app/relay /usr/local/bin/imagent-relay
RUN mkdir -p /var/www/html /var/imagent-uploads
EXPOSE 8099
HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget -qO- http://localhost:8099/health || exit 1
ENTRYPOINT ["/usr/local/bin/imagent-relay"]
CMD ["-port", "8099", "-www", "/var/www/html", "-uploads", "/var/imagent-uploads"]
