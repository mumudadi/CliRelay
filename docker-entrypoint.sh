#!/bin/sh
set -eu

# Cloud Run sets K_SERVICE. Single-container mode can also be forced explicitly.
# Compose multi-service deploys leave these unset and use the external postgres service.
use_embedded_postgres=false
case "${CLIRELAY_EMBEDDED_POSTGRES:-}" in
  1|true|TRUE|yes|YES|on|ON) use_embedded_postgres=true ;;
esac
if [ -n "${K_SERVICE:-}" ]; then
  use_embedded_postgres=true
fi

if [ "$use_embedded_postgres" = "true" ]; then
  if [ -x /usr/local/bin/cloudrun-entrypoint.sh ]; then
    echo "docker-entrypoint: embedded PostgreSQL mode (K_SERVICE=${K_SERVICE:-} CLIRELAY_EMBEDDED_POSTGRES=${CLIRELAY_EMBEDDED_POSTGRES:-})"
    exec /usr/local/bin/cloudrun-entrypoint.sh
  fi
  echo "docker-entrypoint: ERROR: embedded PostgreSQL requested but cloudrun-entrypoint.sh is missing" >&2
  exit 1
fi

case "${1:-}" in
  ./CLIProxyAPI|CLIProxyAPI|/CLIProxyAPI/CLIProxyAPI)
    auth_path="${AUTH_PATH:-/CLIProxyAPI/auths}"
    mkdir -p /CLIProxyAPI/data /CLIProxyAPI/logs "$auth_path"
    case "$auth_path" in
      /root/*) chmod 755 /root ;;
    esac
    chown -R clirelay:clirelay /CLIProxyAPI/data /CLIProxyAPI/logs "$auth_path"
    if [ -e /CLIProxyAPI/config.yaml ]; then
      chown clirelay:clirelay /CLIProxyAPI/config.yaml 2>/dev/null || true
    fi
    exec su-exec clirelay:clirelay "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
