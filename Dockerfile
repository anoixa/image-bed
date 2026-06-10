FROM golang:1.26 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    tzdata \
    build-essential \
    pkg-config \
    libvips-dev \
    libheif-dev \
    libheif-plugin-aomenc \
    libheif-plugin-aomdec \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build metadata. The release workflow passes --build-arg VERSION=release and
# --build-arg COMMIT_HASH=<sha>; both are injected via ldflags so the image
# reports the real version and config.IsProduction() can fall back to it.
ARG VERSION=dev
ARG COMMIT_HASH=""

RUN CGO_ENABLED=1 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X 'github.com/anoixa/image-bed/config.Version=${VERSION}' \
      -X 'github.com/anoixa/image-bed/config.CommitHash=${COMMIT_HASH}'" \
    -o image-bed .

FROM debian:13-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libvips42 \
    libheif1 \
    libheif-plugin-aomenc \
    libheif-plugin-aomdec \
    ca-certificates \
    tzdata \
    gosu \
    wget \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 10001 -s /usr/sbin/nologin appuser

WORKDIR /app

COPY --from=builder /app/image-bed .
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh \
    && mkdir -p /app/data \
    && chown -R appuser:appuser /app/data

USER root

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/system/health || exit 1

# Run in production mode by default: disables unauthenticated pprof/Swagger and
# marks auth cookies Secure. This requires the app to be reached over HTTPS
# (directly or via a TLS-terminating reverse proxy). For local plain-HTTP
# testing, override with `-e APP_ENV=development`.
ENV SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8080 \
    APP_ENV=production \
    DB_TYPE=sqlite \
    DB_FILE_PATH=/app/data/image-bed.db

VOLUME ["/app/data"]

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["serve"]
