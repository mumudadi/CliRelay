# ── Frontend source ──────────────────────────────────────────────────────────
ARG BUILDPLATFORM=linux/amd64
FROM --platform=${BUILDPLATFORM:-linux/amd64} alpine:3.22.0 AS frontend-source

ARG FRONTEND_REPOSITORY=https://github.com/kittors/codeProxy.git
ARG FRONTEND_REF=main
ARG FRONTEND_COMMIT=

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Local `docker compose up -d` from the CliRelay repo should always build the
# current management panel instead of depending on a separately checked out
# `frontend/` directory or an outdated published image. FRONTEND_COMMIT is part
# of this layer on purpose: a moving branch name alone is invisible to Docker's
# cache, so the exact frontend SHA must bust the clone layer.
RUN git clone --depth=1 --branch "${FRONTEND_REF}" "${FRONTEND_REPOSITORY}" frontend \
  && if [ -n "${FRONTEND_COMMIT}" ]; then \
    cd frontend \
    && git fetch --depth=1 origin "${FRONTEND_COMMIT}" \
    && git checkout --detach "${FRONTEND_COMMIT}"; \
  fi

# ── Frontend build ───────────────────────────────────────────────────────────
FROM --platform=${BUILDPLATFORM:-linux/amd64} oven/bun:1 AS frontend-builder

WORKDIR /frontend
COPY --from=frontend-source /src/frontend/ .
ARG UI_VERSION=dev
ARG FRONTEND_REPOSITORY=https://github.com/kittors/codeProxy.git
ARG FRONTEND_REF=main
ARG FRONTEND_COMMIT=none
ARG BUILD_DATE=unknown
ENV VITE_APP_VERSION=${UI_VERSION}
ENV VITE_PANEL_REPOSITORY=${FRONTEND_REPOSITORY}
ENV VITE_PANEL_REF=${FRONTEND_REF}
ENV VITE_PANEL_COMMIT=${FRONTEND_COMMIT}
ENV VITE_PANEL_BUILD_DATE=${BUILD_DATE}
RUN bun install --frozen-lockfile
RUN bun run build

# ── Backend build ────────────────────────────────────────────────────────────
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.26.1-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ARG UI_VERSION=dev
ARG FRONTEND_REF=main
ARG FRONTEND_COMMIT=none

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
  -ldflags="-s -w \
    -X 'github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo.Version=${VERSION}' \
    -X 'github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo.Commit=${COMMIT}' \
    -X 'github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo.BuildDate=${BUILD_DATE}' \
    -X 'github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo.FrontendVersion=${UI_VERSION}' \
    -X 'github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo.FrontendCommit=${FRONTEND_COMMIT}' \
    -X 'github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo.FrontendRef=${FRONTEND_REF}'" \
  -o ./CLIProxyAPI ./cmd/server/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
  -ldflags="-s -w" \
  -o ./clirelay-updater ./cmd/updater/

# ── Runtime ──────────────────────────────────────────────────────────────────
# Includes PostgreSQL so Google Cloud Run / single-container deploys work without
# a sibling database service. Docker Compose still uses external postgres/redis;
# embedded mode activates only when K_SERVICE (Cloud Run) is set or
# CLIRELAY_EMBEDDED_POSTGRES=true.
FROM alpine:3.22.0

RUN apk add --no-cache \
    tzdata \
    ca-certificates \
    docker-cli \
    docker-cli-compose \
    su-exec \
    postgresql \
    postgresql-contrib \
    libpq

RUN addgroup -S -g 10001 clirelay \
  && adduser -S -D -H -u 10001 -h /CLIProxyAPI -s /sbin/nologin -G clirelay clirelay \
  && mkdir -p \
    /CLIProxyAPI/panel \
    /CLIProxyAPI/auths \
    /CLIProxyAPI/logs \
    /CLIProxyAPI/data/postgres \
    /CLIProxyAPI/data/pgstore \
    /run/postgresql \
  && chown -R clirelay:clirelay /CLIProxyAPI \
  && chown -R postgres:postgres /CLIProxyAPI/data/postgres /run/postgresql \
  && chmod 775 /run/postgresql

COPY --from=backend-builder --chown=clirelay:clirelay /app/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI
COPY --from=backend-builder --chown=clirelay:clirelay /app/clirelay-updater /CLIProxyAPI/clirelay-updater
COPY --from=frontend-builder --chown=clirelay:clirelay /frontend/dist/ /CLIProxyAPI/panel/

COPY --chown=clirelay:clirelay config.example.yaml /CLIProxyAPI/config.yaml
COPY --chown=clirelay:clirelay config.example.yaml /CLIProxyAPI/config.example.yaml
COPY --chown=clirelay:clirelay config.cloudrun.yaml /CLIProxyAPI/config.cloudrun.yaml

# Bake .env into the image so CLIRELAY_ADMIN_PASSWORD and CLIRELAY_API_KEY
# are stable across container restarts (no random regeneration).
# WARNING: .env may contain secrets; restrict image distribution accordingly.
COPY --chown=clirelay:clirelay .env /CLIProxyAPI/.env

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY scripts/cloudrun-entrypoint.sh /usr/local/bin/cloudrun-entrypoint.sh
COPY scripts/migrate-sqlite-to-postgres.sh /usr/local/bin/migrate-sqlite-to-postgres.sh
COPY scripts/init-compose-env.sh /usr/local/bin/clirelay-init-env

WORKDIR /CLIProxyAPI

EXPOSE 8080 8317

ENV TZ=Asia/Shanghai \
    MANAGEMENT_PANEL_DIR=/CLIProxyAPI/panel \
    AUTH_PATH=/CLIProxyAPI/auths \
    CLIRELAY_LOCALE=zh \
    CLIRELAY_REDIS_ENABLE=false \
    CLIRELAY_ENABLE_PGSTORE=false \
    CLIRELAY_POSTGRES_USER=cliproxy \
    CLIRELAY_POSTGRES_PASSWORD=cliproxy \
    CLIRELAY_POSTGRES_DB=cliproxy \
    PGDATA=/CLIProxyAPI/data/postgres

RUN chmod +x \
      /usr/local/bin/docker-entrypoint.sh \
      /usr/local/bin/cloudrun-entrypoint.sh \
      /usr/local/bin/migrate-sqlite-to-postgres.sh \
      /usr/local/bin/clirelay-init-env \
  && cp /usr/share/zoneinfo/${TZ} /etc/localtime \
  && echo "${TZ}" > /etc/timezone

USER root

ENTRYPOINT ["docker-entrypoint.sh"]

# Cloud Run ignores Docker HEALTHCHECK; kept for local/compose convenience.
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=5 \
  CMD wget -q -T 3 -O /dev/null "http://127.0.0.1:${PORT:-8317}/healthz" || exit 1

CMD ["./CLIProxyAPI"]
