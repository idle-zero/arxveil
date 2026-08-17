package config

import (
	"os"
)

const (
	defaultServerAddress = ":8080"
	defaultGRPCAddress   = ":9090"
	defaultDatabaseURL   = "postgres://arxveil:arxveil-local-dev@localhost:5432/arxveil?sslmode=disable"
)

type Config struct {
	ServerAddress string
	GRPCAddress   string
	DatabaseURL   string
}

func Load() Config {
	return Config{
		ServerAddress: envOrDefault("ARXVEIL_SERVER_ADDRESS", defaultServerAddress),
		GRPCAddress:   envOrDefault("ARXVEIL_SERVER_GRPC_ADDRESS", defaultGRPCAddress),
		DatabaseURL:   envOrDefault("ARXVEIL_DATABASE_URL", defaultDatabaseURL),
	}
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}
