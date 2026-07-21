package config

const (
	DefaultPanelGitHubRepository      = "https://github.com/kittors/codeProxy"
	DefaultPprofAddr                  = "127.0.0.1:8316"
	DefaultAutoUpdateChannel          = "main"
	DefaultAutoUpdateRepository       = "https://github.com/kittors/CliRelay"
	DefaultAutoUpdateDockerImage      = "ghcr.io/kittors/clirelay"
	DefaultAutoUpdateUpdaterURL       = "http://clirelay-updater:8320"
	DefaultModelRequestBodyMB         = 128
	DefaultRequestBodyDiskThresholdMB = 8

	// EnvAuthPath overrides auth-dir with the path visible inside the running container/process.
	EnvAuthPath = "AUTH_PATH"
	// EnvPort overrides the configured listen port for blue-green deploy slots.
	EnvPort = "CLIRELAY_PORT"
	// EnvLegacyPort keeps the existing Docker installer PORT environment useful.
	EnvLegacyPort = "PORT"
	// EnvAdminPassword overrides the admin bootstrap password (must be >= 12 chars).
	EnvAdminPassword = "CLIRELAY_ADMIN_PASSWORD"
	// EnvAPIKey seeds a permanent API key from environment / .env file.
	// Unlike config-file keys, this survives container restarts because it is
	// re-inserted on every boot if missing from the SQLite store.
	EnvAPIKey = "CLIRELAY_API_KEY"

	// EnvPostgresDSN overrides postgres.dsn for container and secret-managed deployments.
	EnvPostgresDSN = "CLIRELAY_POSTGRES_DSN"
	// EnvRedisEnable overrides redis.enable.
	EnvRedisEnable = "CLIRELAY_REDIS_ENABLE"
	// EnvRedisAddr overrides redis.addr.
	EnvRedisAddr = "CLIRELAY_REDIS_ADDR"
	// EnvRedisPassword overrides redis.password.
	EnvRedisPassword = "CLIRELAY_REDIS_PASSWORD"
	// EnvRedisDB overrides redis.db.
	EnvRedisDB = "CLIRELAY_REDIS_DB"
)
