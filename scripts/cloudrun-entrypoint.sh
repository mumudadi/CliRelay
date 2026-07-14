#!/bin/sh
# Single-container entrypoint for Google Cloud Run (and local all-in-one demos).
# Starts an embedded PostgreSQL on 127.0.0.1, then launches CLIProxyAPI.
# Redis stays disabled unless the operator explicitly enables it.

set -eu

log() {
  echo "cloudrun-entrypoint: $*"
}

die() {
  echo "cloudrun-entrypoint: ERROR: $*" >&2
  exit 1
}

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
PG_BINDIR="${PG_BINDIR:-}"

export AUTH_PATH
export CLIRELAY_REDIS_ENABLE="${CLIRELAY_REDIS_ENABLE:-false}"

# Cloud Run injects PORT. Prefer it over any baked-in config port.
if [ -z "${PORT:-}" ]; then
  export PORT=8317
fi
# ApplyEnvOverrides reads CLIRELAY_PORT first, then PORT.
export CLIRELAY_PORT="${PORT}"

# One secret can unlock both identity bootstrap and management API.
if [ -n "${CLIRELAY_ADMIN_PASSWORD:-}" ] && [ -z "${MANAGEMENT_PASSWORD:-}" ]; then
  export MANAGEMENT_PASSWORD="${CLIRELAY_ADMIN_PASSWORD}"
fi
if [ -n "${MANAGEMENT_PASSWORD:-}" ] && [ -z "${CLIRELAY_ADMIN_PASSWORD:-}" ]; then
  export CLIRELAY_ADMIN_PASSWORD="${MANAGEMENT_PASSWORD}"
fi

# On Cloud Run, prefer the cloud-friendly config template once (first boot only).
if [ -n "${K_SERVICE:-}" ] && [ -f "${APP_DIR}/config.cloudrun.yaml" ]; then
  if [ ! -f "${DATA_DIR}/.cloudrun-config-applied" ]; then
    log "applying config.cloudrun.yaml for Cloud Run service ${K_SERVICE}"
    cp "${APP_DIR}/config.cloudrun.yaml" "${APP_DIR}/config.yaml"
    mkdir -p "$DATA_DIR"
    touch "${DATA_DIR}/.cloudrun-config-applied"
  fi
fi

command -v su-exec >/dev/null 2>&1 || die "su-exec is required"
command -v initdb >/dev/null 2>&1 || die "postgresql client/server packages are missing (initdb not found)"
command -v pg_ctl >/dev/null 2>&1 || die "postgresql packages are missing (pg_ctl not found)"

# Resolve postgres binary directory for Alpine (versioned path under /usr/libexec or /usr/bin).
if [ -z "$PG_BINDIR" ]; then
  if command -v postgres >/dev/null 2>&1; then
    PG_BINDIR="$(dirname "$(command -v postgres)")"
  elif [ -x /usr/libexec/postgresql17/postgres ]; then
    PG_BINDIR=/usr/libexec/postgresql17
  elif [ -x /usr/libexec/postgresql16/postgres ]; then
    PG_BINDIR=/usr/libexec/postgresql16
  elif [ -x /usr/libexec/postgresql15/postgres ]; then
    PG_BINDIR=/usr/libexec/postgresql15
  fi
fi
if [ -n "$PG_BINDIR" ]; then
  export PATH="${PG_BINDIR}:${PATH}"
fi

mkdir -p "$DATA_DIR" "$LOG_DIR" "$AUTH_PATH" "$PGDATA" "$PG_RUNDIR"
chown -R clirelay:clirelay "$LOG_DIR" "$AUTH_PATH" 2>/dev/null || true
chown -R postgres:postgres "$PGDATA" "$PG_RUNDIR" 2>/dev/null || true
chmod 700 "$PGDATA" 2>/dev/null || true
chmod 775 "$PG_RUNDIR" 2>/dev/null || true

build_dsn() {
  if [ -n "${CLIRELAY_POSTGRES_DSN:-}" ]; then
    log "using provided CLIRELAY_POSTGRES_DSN"
    return 0
  fi
  export CLIRELAY_POSTGRES_DSN="postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=disable"
  log "set CLIRELAY_POSTGRES_DSN for embedded postgres"
}

wait_for_postgres() {
  i=0
  while [ "$i" -lt 90 ]; do
    if su-exec postgres pg_isready -p "$PG_PORT" -q 2>/dev/null \
      || su-exec postgres pg_isready -h "$PG_HOST" -p "$PG_PORT" -q 2>/dev/null; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.5
  done
  log "PostgreSQL did not become ready in time"
  if [ -f "${LOG_DIR}/postgres.log" ]; then
    log "--- postgres.log (tail) ---"
    tail -n 100 "${LOG_DIR}/postgres.log" || true
  fi
  return 1
}

# Bootstrap admin connections MUST use the Unix socket.
# pg_hba has "local all all trust"; TCP to 127.0.0.1 requires a password
# and the superuser has none after initdb.
psql_superuser() {
  # -h <socket dir> forces local/unix peer path (trust), never TCP.
  su-exec postgres \
    env PGHOST= PGHOSTADDR= PGPASSWORD= \
    psql --no-password -v ON_ERROR_STOP=1 \
      -h "$PG_RUNDIR" -p "$PG_PORT" -U postgres "$@"
}

# Quote a string as a PostgreSQL string literal ('...' with ' doubled).
# Do NOT use psql :'var' interpolation here — under su-exec/env it was sent
# literally to the server as PASSWORD :'pass' (syntax error).
sql_string_literal() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"
}

init_postgres() {
  if [ -s "${PGDATA}/PG_VERSION" ]; then
    log "reusing existing PGDATA at ${PGDATA}"
    return 0
  fi

  log "initializing PostgreSQL data directory at ${PGDATA}"
  # Cloud Run /dev/shm is small; keep memory footprint tiny.
  su-exec postgres initdb \
    -D "$PGDATA" \
    --auth-local=trust \
    --auth-host=scram-sha-256 \
    --encoding=UTF8 \
    --locale=C \
    --username=postgres \
    >/dev/null

  cat >> "${PGDATA}/postgresql.conf" <<EOF

# CliRelay single-container defaults (Cloud Run friendly, low /dev/shm)
listen_addresses = '127.0.0.1'
port = ${PG_PORT}
unix_socket_directories = '${PG_RUNDIR}'
shared_buffers = 16MB
work_mem = 1MB
maintenance_work_mem = 16MB
effective_cache_size = 64MB
max_connections = 20
wal_level = minimal
max_wal_senders = 0
# wal_segment_size defaults to 16MB; min_wal_size must be >= 2 * segment size.
max_wal_size = 64MB
min_wal_size = 32MB
synchronous_commit = off
fsync = off
full_page_writes = off
checkpoint_timeout = 30min
log_min_messages = warning
dynamic_shared_memory_type = posix
EOF

  cat > "${PGDATA}/pg_hba.conf" <<EOF
local   all             all                                     trust
host    all             all             127.0.0.1/32            scram-sha-256
host    all             all             ::1/128                 scram-sha-256
EOF
}

ensure_role_and_db() {
  log "bootstrapping role/db via Unix socket (local trust)"
  pass_sql="$(sql_string_literal "$PG_PASSWORD")"

  role_exists="$(psql_superuser -tAc \
    "SELECT 1 FROM pg_roles WHERE rolname='${PG_USER}'" | tr -d '[:space:]' || true)"
  if [ "$role_exists" = "1" ]; then
    log "updating password for role ${PG_USER}"
    psql_superuser -c "ALTER ROLE \"${PG_USER}\" WITH LOGIN PASSWORD ${pass_sql};" \
      || die "ALTER ROLE ${PG_USER} failed"
  else
    log "creating role ${PG_USER}"
    psql_superuser -c "CREATE ROLE \"${PG_USER}\" LOGIN PASSWORD ${pass_sql};" \
      || die "CREATE ROLE ${PG_USER} failed"
  fi

  db_exists="$(psql_superuser -tAc \
    "SELECT 1 FROM pg_database WHERE datname='${PG_DB}'" | tr -d '[:space:]' || true)"
  if [ "$db_exists" != "1" ]; then
    log "creating database ${PG_DB}"
    psql_superuser -c "CREATE DATABASE \"${PG_DB}\" OWNER \"${PG_USER}\";" \
      || die "CREATE DATABASE ${PG_DB} failed"
  fi

  psql_superuser -c "GRANT ALL PRIVILEGES ON DATABASE \"${PG_DB}\" TO \"${PG_USER}\";" \
    || die "GRANT on database ${PG_DB} failed"

  # On PG 15+ also grant schema privileges on the app database.
  su-exec postgres \
    env PGHOST= PGHOSTADDR= PGPASSWORD= \
    psql --no-password -v ON_ERROR_STOP=1 \
      -h "$PG_RUNDIR" -p "$PG_PORT" -U postgres -d "$PG_DB" \
      -c "GRANT ALL ON SCHEMA public TO \"${PG_USER}\";" \
      -c "ALTER SCHEMA public OWNER TO \"${PG_USER}\";" \
    >/dev/null 2>&1 || true

  # Verify the app DSN path (TCP + password) before starting CLIProxyAPI.
  log "verifying TCP auth for ${PG_USER}@${PG_HOST}:${PG_PORT}/${PG_DB}"
  if ! env PGPASSWORD="$PG_PASSWORD" \
    psql --no-password -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" \
    -v ON_ERROR_STOP=1 -tAc "SELECT 1" >/dev/null; then
    die "TCP login failed for role ${PG_USER}; embedded bootstrap incomplete"
  fi
  log "role/db bootstrap ok"
}

start_postgres() {
  init_postgres
  log "starting PostgreSQL on ${PG_HOST}:${PG_PORT}"
  : > "${LOG_DIR}/postgres.log"
  chown postgres:postgres "${LOG_DIR}/postgres.log" 2>/dev/null || true
  tail -F "${LOG_DIR}/postgres.log" 2>/dev/null &
  TAIL_PID=$!

  if ! su-exec postgres pg_ctl \
    -D "$PGDATA" \
    -l "${LOG_DIR}/postgres.log" \
    -o "-c listen_addresses=${PG_HOST} -c port=${PG_PORT} -c unix_socket_directories=${PG_RUNDIR} -c shared_buffers=16MB -c max_connections=20" \
    -w start; then
    log "pg_ctl start failed"
    tail -n 100 "${LOG_DIR}/postgres.log" || true
    kill "$TAIL_PID" 2>/dev/null || true
    return 1
  fi
  wait_for_postgres || {
    kill "$TAIL_PID" 2>/dev/null || true
    return 1
  }
  ensure_role_and_db || die "failed to bootstrap embedded PostgreSQL role/database"
  log "PostgreSQL is ready"
}

stop_postgres() {
  if su-exec postgres pg_ctl -D "$PGDATA" status >/dev/null 2>&1; then
    log "stopping PostgreSQL"
    su-exec postgres pg_ctl -D "$PGDATA" -m fast -w stop || true
  fi
}

APP_PID=""
TAIL_PID=""

shutdown() {
  log "received shutdown signal"
  if [ -n "${APP_PID:-}" ] && kill -0 "$APP_PID" 2>/dev/null; then
    kill -TERM "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
  fi
  stop_postgres
  if [ -n "${TAIL_PID:-}" ]; then
    kill "$TAIL_PID" 2>/dev/null || true
  fi
  exit 0
}

trap shutdown INT TERM

log "boot: PORT=${PORT} CLIRELAY_PORT=${CLIRELAY_PORT} K_SERVICE=${K_SERVICE:-} PGDATA=${PGDATA}"
log "memory/shm: /dev/shm size follows"
df -h /dev/shm 2>/dev/null || true

start_postgres || die "failed to start embedded PostgreSQL"
build_dsn

if [ "${CLIRELAY_ENABLE_PGSTORE:-false}" = "true" ] && [ -z "${PGSTORE_DSN:-}" ]; then
  export PGSTORE_DSN="${CLIRELAY_POSTGRES_DSN}"
  export PGSTORE_LOCAL_PATH="${PGSTORE_LOCAL_PATH:-${DATA_DIR}/pgstore}"
  mkdir -p "$PGSTORE_LOCAL_PATH"
  chown -R clirelay:clirelay "$PGSTORE_LOCAL_PATH"
fi

if [ -e "${APP_DIR}/config.yaml" ]; then
  chown clirelay:clirelay "${APP_DIR}/config.yaml" 2>/dev/null || true
fi

chown -R clirelay:clirelay "$AUTH_PATH" "$LOG_DIR" "${DATA_DIR}/pgstore" 2>/dev/null || true

log "starting CLIProxyAPI on 0.0.0.0:${CLIRELAY_PORT} (redis=${CLIRELAY_REDIS_ENABLE})"
cd "$APP_DIR"

su-exec clirelay:clirelay ./CLIProxyAPI &
APP_PID=$!

i=0
while [ "$i" -lt 120 ]; do
  if ! kill -0 "$APP_PID" 2>/dev/null; then
    wait "$APP_PID" || true
    die "CLIProxyAPI exited during startup (check postgres DSN / config logs above)"
  fi
  if (command -v wget >/dev/null 2>&1 && wget -q -T 1 -O /dev/null "http://127.0.0.1:${PORT}/healthz" 2>/dev/null) \
    || (command -v nc >/dev/null 2>&1 && nc -z 127.0.0.1 "$PORT" 2>/dev/null); then
    log "CLIProxyAPI is accepting connections on port ${PORT}"
    break
  fi
  i=$((i + 1))
  sleep 0.5
done

wait "$APP_PID"
status=$?
log "CLIProxyAPI exited with status ${status}"
stop_postgres
if [ -n "${TAIL_PID:-}" ]; then
  kill "$TAIL_PID" 2>/dev/null || true
fi
exit "$status"
