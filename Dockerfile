FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata build-base vips-dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o image-bed .

FROM alpine:edge

RUN apk add --no-cache \
    vips \
    ca-certificates \
    tzdata \
    && rm -rf /var/cache/apk/*

RUN adduser -D -s /bin/sh appuser

WORKDIR /app

COPY --from=builder /app/image-bed .

RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENV SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8080 \
    DB_TYPE=sqlite \
    DB_FILE_PATH=/app/data/image-bed.db

VOLUME ["/app/data"]

ENTRYPOINT ["./image-bed"]
CMD ["serve"]
