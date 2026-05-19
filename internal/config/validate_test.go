package config_test

import (
	"strings"
	"testing"

	"github.com/mitigador/mitigador/internal/config"
)

func validCfg() *config.Config {
	return &config.Config{
		Postgres: config.Postgres{DSN: "postgres://x", MaxConns: 16, MinConns: 2},
		HTTP: config.HTTP{
			ListenAddr:    "0.0.0.0",
			ListenPort:    8080,
			SessionSecret: "0123456789abcdef0123456789abcdef",
			AppBaseURL:    "https://mitigador.example.com",
		},
		Ingest: config.Ingest{
			NetFlow:            config.IngestPort{ListenAddr: "0.0.0.0", ListenPort: 2055},
			IPFIX:              config.IngestPort{ListenAddr: "0.0.0.0", ListenPort: 4739},
			SFlow:              config.IngestPort{ListenAddr: "0.0.0.0", ListenPort: 6343},
			ReceiveBufferBytes: 33554432,
		},
		Telegram: config.Telegram{BotToken: "1234567890:ABCDEFGHIJKLMNOPQRST", AllowedChatIDs: []int64{1}},
		SMTP: config.SMTP{
			Host:     "smtp.example.com",
			Port:     587,
			Username: "u",
			Password: "p",
			Security: "starttls",
			FromAddr: "a@b.com",
			ToAddrs:  []string{"c@d.com"},
		},
		Log: config.Log{Level: "info", Format: "json"},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := config.Validate(validCfg()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_MissingDSN(t *testing.T) {
	c := validCfg()
	c.Postgres.DSN = ""
	err := config.Validate(c)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "DSN") {
		t.Errorf("expected DSN in error: %v", err)
	}
}

func TestValidate_ShortSessionSecret(t *testing.T) {
	c := validCfg()
	c.HTTP.SessionSecret = "tooshort"
	err := config.Validate(c)
	if err == nil || !strings.Contains(err.Error(), "min") {
		t.Errorf("expected min-length error, got %v", err)
	}
}

func TestValidate_EmptyChatIDs(t *testing.T) {
	c := validCfg()
	c.Telegram.AllowedChatIDs = nil
	err := config.Validate(c)
	if err == nil || !strings.Contains(err.Error(), "AllowedChatIDs") {
		t.Errorf("expected AllowedChatIDs error, got %v", err)
	}
}

func TestValidate_InvalidSecurity(t *testing.T) {
	c := validCfg()
	c.SMTP.Security = "weirdmode"
	err := config.Validate(c)
	if err == nil || !strings.Contains(err.Error(), "Security") {
		t.Errorf("expected Security error, got %v", err)
	}
}
