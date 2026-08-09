package config

import "os"

const (
	defaultServerAddress = ":8080"
	defaultDatabaseURL   = "postgres://arxveil:arxveil-local-dev@localhost:5432/arxveil?"
)

type Config struct {
	ServerAddress string
	DatabaseURL   string
}

func Load() Config {
	return Config{
		ServerAddress: envOrDefault("ARXVEIL_SERVER_ADDRESS", defaultServerAddress),
		DatabaseURL:   envOrDefault("ARXVEIL_DATABASE_URL", defaultDatabaseURL),
	}
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}
