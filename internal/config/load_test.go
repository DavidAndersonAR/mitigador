package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mitigador/mitigador/internal/config"
)

func TestLoad_Valid(t *testing.T) {
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Postgres.DSN == "" {
		t.Error("expected Postgres.DSN populated")
	}
	if cfg.HTTP.ListenPort != 8080 {
		t.Errorf("HTTP.ListenPort = %d, want 8080", cfg.HTTP.ListenPort)
	}
	if cfg.Ingest.NetFlow.ListenPort != 2055 {
		t.Errorf("Ingest.NetFlow.ListenPort = %d, want 2055", cfg.Ingest.NetFlow.ListenPort)
	}
	if len(cfg.Telegram.AllowedChatIDs) != 1 || cfg.Telegram.AllowedChatIDs[0] != 123456789 {
		t.Errorf("Telegram.AllowedChatIDs = %v, want [123456789]", cfg.Telegram.AllowedChatIDs)
	}
}

func TestLoad_NonExistent(t *testing.T) {
	_, err := config.Load("testdata/this-file-does-not-exist.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "this-file-does-not-exist.yaml") {
		t.Errorf("error %q does not mention the missing filename", err)
	}
}

func TestLoad_MissingSecret(t *testing.T) {
	_, err := config.Load("testdata/missing_secret.yaml")
	if err == nil {
		t.Fatal("expected error for missing session_secret, got nil")
	}
	if !strings.Contains(err.Error(), "SessionSecret") {
		t.Errorf("error should mention SessionSecret; got: %v", err)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("MITIGADOR_POSTGRES_DSN", "postgres://override:x@127.0.0.1:5432/override_db?sslmode=disable")
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.Postgres.DSN, "override_db") {
		t.Errorf("env override not applied: DSN=%s", cfg.Postgres.DSN)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
