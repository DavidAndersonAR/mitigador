package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// DefaultPath is the canonical operator config location.
const DefaultPath = "/etc/mitigador/config.yaml"

// Load reads the YAML at path, applies MITIGADOR_<SECTION>_<KEY> env overrides,
// and validates the result. Returns a typed Config or an error.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Defaults.
	v.SetDefault("postgres.max_conns", int32(16))
	v.SetDefault("postgres.min_conns", int32(2))
	v.SetDefault("ingest.receive_buffer_bytes", 33554432)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	// Env overrides: MITIGADOR_POSTGRES_DSN, MITIGADOR_TELEGRAM_BOT_TOKEN, etc.
	v.SetEnvPrefix("MITIGADOR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
