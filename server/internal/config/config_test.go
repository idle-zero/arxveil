package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {

	got := Load()

	if got.ServerAddress != defaultServerAddress {
		t.Errorf("ServerAddress = %q, want %q", got.ServerAddress, defaultServerAddress)
	}

	if got.DatabaseURL != defaultDatabaseURL {
		t.Errorf("DatabaseURL = %q, want %q", got.DatabaseURL, defaultDatabaseURL)
	}
}

func TestLoadUsesEnvOverrides(t *testing.T) {
	t.Setenv("ARXVEIL_SERVER_ADDRESS", ":9090")
	t.Setenv("ARXVEIL_DATABASE_URL", "postgres://example")

	got := Load()

	if got.ServerAddress != ":9090" {
		t.Errorf("ServerAddress = %q, want %q", got.ServerAddress, ":9090")
	}

	if got.DatabaseURL != "postgres://example" {
		t.Errorf("DatabaseURL = %q, want %q", got.DatabaseURL, "postgres://example")
	}
}
