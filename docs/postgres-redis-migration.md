# PostgreSQL and Redis Runtime Data Stack

CliRelay now uses PostgreSQL 15+ as the runtime primary database. Redis 7+ is used only for cache, locks, limits, queues, and rebuildable runtime snapshots. Business facts such as API keys, routing, proxy pool, request logs, request log content, model config, pricing, identity fingerprints, and quota records must be recoverable from PostgreSQL.

## Configure

For Docker Compose, the bundled `docker-compose.yml` starts `postgres:15-alpine` and `redis:7-alpine`. The application container receives:

- `CLIRELAY_POSTGRES_DSN`
- `CLIRELAY_REDIS_ENABLE`
- `CLIRELAY_REDIS_ADDR`
- `CLIRELAY_REDIS_PASSWORD`
- `CLIRELAY_REDIS_DB`

For non-Compose deployments, set the same values in the environment or edit `postgres.dsn` and `redis.*` in `config.yaml`.

## Deploy Gate

Pushes to `dev` build the Linux binary but do not deploy to the server unless repository variable `CLIRELAY_DEV_AUTO_DEPLOY` is set to `true`. Prefer leaving it disabled for this migration and running the deploy workflow manually after PostgreSQL, Redis, SQLite import dry-run/apply, and row-count/checksum checks are complete.

The blue-green deploy script refuses to proceed unless `postgres.dsn` or `CLIRELAY_POSTGRES_DSN` is configured. If Redis is enabled, it also requires `redis.addr` or `CLIRELAY_REDIS_ADDR`.

## Inspect Legacy SQLite

Run the read-only inventory before importing any old `usage.db`:

```bash
./cli-proxy-api -sqlite-dry-run /path/to/usage.db
```

The command opens SQLite read-only and prints table names, columns, row counts, numeric ID ranges, time ranges, checksums, and `dry_run_only: true`. It does not print row contents.

Run the import dry-run against PostgreSQL before applying:

```bash
CLIRELAY_POSTGRES_DSN='postgres://user:pass@127.0.0.1:5432/cliproxy?sslmode=disable' \
./cli-proxy-api -sqlite-import /path/to/usage.db
```

The import command runs PostgreSQL migrations, compares source/target columns, row counts, and checksums, and reports planned inserts without writing by default. Apply only after the report is reviewed:

```bash
CLIRELAY_POSTGRES_DSN='postgres://user:pass@127.0.0.1:5432/cliproxy?sslmode=disable' \
./cli-proxy-api -sqlite-import /path/to/usage.db -sqlite-import-dry-run=false
```

The apply path writes in batches, uses `ON CONFLICT DO NOTHING`, and resets PostgreSQL identity sequences for imported tables with generated IDs.

## Validate PostgreSQL

Run Ent codegen and tests:

```bash
rtk go generate ./internal/storage/postgres/ent
rtk go test ./internal/storage/postgres/... ./internal/cmd -count=1
rtk go test ./internal/usage -count=1
rtk go test ./...
```

With PostgreSQL 15+ and Redis 7+ available:

```bash
CLIRELAY_POSTGRES_TEST_DSN='postgres://cliproxy:cliproxy@127.0.0.1:55432/cliproxy?sslmode=disable' \
CLIRELAY_REDIS_TEST_ADDR='127.0.0.1:56379' \
rtk go test ./internal/usage -run TestPostgresRuntimeDataStackIntegration -count=1 -v

CLIRELAY_POSTGRES_TEST_DSN='postgres://cliproxy:cliproxy@127.0.0.1:55432/cliproxy?sslmode=disable' \
rtk go test ./internal/storage/postgres/sqliteinventory -run TestImportSQLiteDryRunAndApply -count=1 -v
```

## Rollback Boundary

Keep the original SQLite file as a read-only backup until row counts, ID/time ranges, checksums, key CRUD, management queries, request log content, cache fallback, and quota/identity paths have been verified against PostgreSQL. Do not run the migrated service against SQLite as a runtime primary database.
