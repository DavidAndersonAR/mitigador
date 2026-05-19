// Package config loads YAML + env into the operator config struct.
//
// Filled in plan 01-03; see .planning/phases/01-observation-spine/01-03-PLAN.md.
package config

// Config is the root operator config.
type Config struct {
	Postgres Postgres `mapstructure:"postgres" validate:"required"`
	HTTP     HTTP     `mapstructure:"http"     validate:"required"`
	Ingest   Ingest   `mapstructure:"ingest"   validate:"required"`
	Telegram Telegram `mapstructure:"telegram" validate:"required"`
	SMTP     SMTP     `mapstructure:"smtp"     validate:"required"`
	Log      Log      `mapstructure:"log"`
	GeoIP    GeoIP    `mapstructure:"geoip"`
}

// GeoIP optionally enriches dashboard rows with AS organization names.
// Empty ASNPath = feature disabled; the CIDR fallback table still works.
type GeoIP struct {
	ASNPath string `mapstructure:"asn_path"`
}

// Postgres holds DB connection settings.
type Postgres struct {
	DSN      string `mapstructure:"dsn"       validate:"required,startswith=postgres"`
	MaxConns int32  `mapstructure:"max_conns" validate:"gte=1,lte=200"`
	MinConns int32  `mapstructure:"min_conns" validate:"gte=1,lte=200"`
}

// HTTP holds the API/dashboard server settings.
type HTTP struct {
	ListenAddr    string `mapstructure:"listen_addr"    validate:"required,ip"`
	ListenPort    int    `mapstructure:"listen_port"    validate:"required,gte=1,lte=65535"`
	SessionSecret string `mapstructure:"session_secret" validate:"required,min=32"`
	AppBaseURL    string `mapstructure:"app_base_url"   validate:"required,url"`
}

// Ingest holds the three UDP listener configurations and shared buffer size.
type Ingest struct {
	NetFlow            IngestPort `mapstructure:"netflow" validate:"required"`
	IPFIX              IngestPort `mapstructure:"ipfix"   validate:"required"`
	SFlow              IngestPort `mapstructure:"sflow"   validate:"required"`
	ReceiveBufferBytes int        `mapstructure:"receive_buffer_bytes" validate:"gte=1048576"`
}

// IngestPort is one UDP listener's bind + port.
type IngestPort struct {
	ListenAddr string `mapstructure:"listen_addr" validate:"required,ip"`
	ListenPort int    `mapstructure:"listen_port" validate:"required,gte=1,lte=65535"`
}

// Telegram holds the bot config and chat allowlist.
type Telegram struct {
	BotToken       string  `mapstructure:"bot_token"        validate:"required,min=20"`
	AllowedChatIDs []int64 `mapstructure:"allowed_chat_ids" validate:"required,min=1,dive,required"`
}

// SMTP holds the relay config and email recipients.
type SMTP struct {
	Host     string   `mapstructure:"host"      validate:"required,hostname|ip"`
	Port     int      `mapstructure:"port"      validate:"required,gte=1,lte=65535"`
	Username string   `mapstructure:"username"  validate:"required"`
	Password string   `mapstructure:"password"  validate:"required"`
	Security string   `mapstructure:"security"  validate:"required,oneof=starttls tls plain"`
	FromAddr string   `mapstructure:"from_addr" validate:"required,email"`
	ToAddrs  []string `mapstructure:"to_addrs"  validate:"required,min=1,dive,email"`
}

// Log holds the log format and level.
type Log struct {
	Level  string `mapstructure:"level"  validate:"oneof=debug info warn error"`
	Format string `mapstructure:"format" validate:"oneof=json text"`
}
