#!/bin/sh
# Single-container entrypoint for Google Cloud Run (and local all-in-one demos).
# Starts an embedded PostgreSQL on 127.0.0.1, then launches CLIProxyAPI.
# Redis is intentionally left disabled; Cloud Run has no sibling Redis service.

set -eu

APP_DIR="${APP_DIR:-/CLIProxyAPI}"
DATA_DIR="${CLIRELAY_DATA_DIR:-${APP_DIR}/data}"
LOG_DIR="${CLIRELAY_LOG_DIR:-${APP_DIR}/logs}"
AUTH_PATH="${AUTH_PATH:-${APP_DIR}/auths}"
PGDATA="${PGDATA:-${DATA_DIR}/postgres}"
PG_RUNDIR="${PG_RUNDIR:-/run/postgresql}"
PG_USER="${CLIRELAY_POSTGRES_USER:-cliproxy}"
PG_PASSWORD="${CLIRELAY_POSTGRES_PASSWORD:-cliproxy}"
PG_DB="${CLIRELAY_POSTGRES_DB:-cliproxy}"
PG_PORT="${CLIRELAY_POSTGRES_PORT:-5432}"
PG_HOST="127.0.0.1"

export AUTH_PATH
export CLIRELAY_REDIS_ENABLE="${CLIRELAY_REDIS_ENABLE:-false}"

# Cloud Run injects PORT; fall back to the app default when running locally.
if [ -z "${PORT:-}" ]; then
  export PORT=8317
fi
export CLIRELAY_PORT="${CLIRELAY_PORT:-$PORT}"

# Allow a single secret for both admin bootstrap and management API.
if [ -n "${CLIRELAY_ADMIN_PASSWORD:-}" ] && [ -z "${MANAGEMENT_PASSWORD:-}" ]; then
  export MANAGEMENT_PASSWORD="${CLIRELAY_ADMIN_PASSWORD}"
fi
if [ -n "${MANAGEMENT_PASSWORD:-}" ] && [ -z "${CLIRELAY_ADMIN_PASSWORD:-}" ]; then
  export CLIRELAY_ADMIN_PASSWORD="${MANAGEMENT_PASSWORD}"
fi

mkdir -p "$DATA_DIR" "$LOG_DIR" "$AUTH_PATH" "$PGDATA" "$PG_RUNDIR"
chown -R clirelay:clirelay "$LOG_DIR" "$AUTH_PATH" 2>/dev/null || true
chown -R postgres:postgres "$PGDATA" "$PG_RUNDIR"
chmod 700 "$PGDATA"
chmod 775 "$PG_RUNDIR"

build_dsn() {
  if [ -n "${CLIRELAY_POSTGRES_DSN:-}" ]; then
    return 0
  fi
  export CLIRELAY_POSTGRES_DSN="postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=disable"
}

wait_for_postgres() {
  i=0
  while [ "$i" -lt 60 ]; do
    if su-exec postgres pg_isready -h "$PG_HOST" -p "$PG_PORT" -q; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.5
  done
  echo "cloudrun-entrypoint: PostgreSQL did not become ready in time" >&2
  if [ -f "${LOG_DIR}/postgres.log" ]; then
    tail -n 80 "${LOG_DIR}/postgres.log" >&2 || true
  fi
  return 1
}

init_postgres() {
  if [ -s "${PGDATA}/PG_VERSION" ]; then
    return 0
  fi

  echo "cloudrun-entrypoint: initializing PostgreSQL data directory at ${PGDATA}"
  su-exec postgres initdb \
    -D "$PGDATA" \
    --auth-local=trust \
    --auth-host=scram-sha-256 \
    --encoding=UTF8 \
    --locale=C \
    --username=postgres \
    >/dev/null

  cat >> "${PGDATA}/postgresql.conf" <<EOF

# CliRelay single-container defaults (Cloud Run friendly)
listen_addresses = '127.0.0.1'
port = ${PG_PORT}
unix_socket_directories = '${PG_RUNDIR}'
shared_buffers = 64MB
work_mem = 4MB
maintenance_work_mem = 64MB
max_connections = 50
wal_level = minimal
max_wal_senders = 0
synchronous_commit = off
fsync = off
full_page_writes = off
checkpoint_timeout = 15min
log_min_messages = warning
EOF

  cat > "${PGDATA}/pg_hba.conf" <<EOF
local   all             all                                     trust
host    all             all             127.0.0.1/32            scram-sha-256
host    all             all             ::1/128                 scram-sha-256
EOF
}

# Create/update app role and database using psql variables (safe for most passwords).
ensure_role_and_db() {
  role_exists="$(su-exec postgres psql -h "$PG_HOST" -p "$PG_PORT" -U postgres -tAc \
    "SELECT 1 FROM pg_roles WHERE rolname='${PG_USER}'" | tr -d '[:space:]')"
  if [ "$role_exists" = "1" ]; then
    su-exec postgres psql -h "$PG_HOST" -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 \
      -v pass="$PG_PASSWORD" \
      -c "ALTER ROLE \"${PG_USER}\" WITH LOGIN PASSWORD :'pass';"
  else
    su-exec postgres psql -h "$PG_HOST" -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 \
      -v pass="$PG_PASSWORD" \
      -c "CREATE ROLE \"${PG_USER}\" LOGIN PASSWORD :'pass';"
  fi

  db_exists="$(su-exec postgres psql -h "$PG_HOST" -p "$PG_PORT" -U postgres -tAc \
    "SELECT 1 FROM pg_database WHERE datname='${PG_DB}'" | tr -d '[:space:]')"
  if [ "$db_exists" != "1" ]; then
    su-exec postgres psql -h "$PG_HOST" -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 \
      -c "CREATE DATABASE \"${PG_DB}\" OWNER \"${PG_USER}\";"
  fi

  su-exec postgres psql -h "$PG_HOST" -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 \
    -c "GRANT ALL PRIVILEGES ON DATABASE \"${PG_DB}\" TO \"${PG_USER}\";"
}

start_postgres() {
  init_postgres
  echo "cloudrun-entrypoint: starting PostgreSQL on ${PG_HOST}:${PG_PORT}"
  su-exec postgres pg_ctl \
    -D "$PGDATA" \
    -l "${LOG_DIR}/postgres.log" \
    -o "-c listen_addresses=${PG_HOST} -c port=${PG_PORT} -c unix_socket_directories=${PG_RUNDIR}" \
    -w start
  wait_for_postgres
  ensure_role_and_db
}

stop_postgres() {
  if su-exec postgres pg_ctl -D "$PGDATA" status >/dev/null 2>&1; then
    echo "cloudrun-entrypoint: stopping PostgreSQL"
    su-exec postgres pg_ctl -D "$PGDATA" -m fast -w stop || true
  fi
}

APP_PID=""

shutdown() {
  echo "cloudrun-entrypoint: received shutdown signal"
  if [ -n "$APP_PID" ] && kill -0 "$APP_PID" 2>/dev/null; then
    kill -TERM "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
  fi
  stop_postgres
  exit 0
}

trap shutdown INT TERM

start_postgres
build_dsn

# Optional: mirror config/auth into the same embedded DB (opt-in; first bootstrap uses config.example.yaml).
if [ "${CLIRELAY_ENABLE_PGSTORE:-false}" = "true" ] && [ -z "${PGSTORE_DSN:-}" ]; then
  export PGSTORE_DSN="${CLIRELAY_POSTGRES_DSN}"
  export PGSTORE_LOCAL_PATH="${PGSTORE_LOCAL_PATH:-${DATA_DIR}/pgstore}"
  mkdir -p "$PGSTORE_LOCAL_PATH"
  chown -R clirelay:clirelay "$PGSTORE_LOCAL_PATH"
fi

if [ -e "${APP_DIR}/config.yaml" ]; then
  chown clirelay:clirelay "${APP_DIR}/config.yaml" 2>/dev/null || true
fi

echo "cloudrun-entrypoint: starting CLIProxyAPI on port ${CLIRELAY_PORT} (redis=${CLIRELAY_REDIS_ENABLE})"
cd "$APP_DIR"
su-exec clirelay:clirelay ./CLIProxyAPI &
APP_PID=$!

wait "$APP_PID"
status=$?
stop_postgres
exit "$status"
