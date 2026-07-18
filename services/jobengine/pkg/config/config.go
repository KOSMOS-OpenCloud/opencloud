package config

import (
	"context"

	"github.com/opencloud-eu/opencloud/pkg/shared"
)

// Config combines all available configuration parts.
type Config struct {
	Commons *shared.Commons `yaml:"-"`

	Service Service `yaml:"-"`

	LogLevel string `yaml:"loglevel" env:"OC_LOG_LEVEL;JOBENGINE_LOG_LEVEL"`

	Debug Debug `yaml:"debug"`
	HTTP  HTTP  `yaml:"http"`

	TokenManager *TokenManager `yaml:"token_manager"`
	Events       Events        `yaml:"events"`

	Context context.Context `yaml:"-"`

	MaxWorkers   int      `yaml:"max_workers" env:"JOBENGINE_MAX_WORKERS"`
	QueueSize    int      `yaml:"queue_size" env:"JOBENGINE_QUEUE_SIZE"`
	TempDir      string   `yaml:"temp_dir" env:"JOBENGINE_TEMP_DIR"`
	PipelineDirs []string `yaml:"pipeline_dirs" env:"JOBENGINE_PIPELINE_DIRS"`
	ConfigFile   string   `yaml:"config_file" env:"JOBENGINE_CONFIG_FILE"`
	MatrixFile   string   `yaml:"matrix_file" env:"JOBENGINE_MATRIX_FILE"`
}

// Service defines the service name.
type Service struct {
	Name string `yaml:"-"`
}

// Debug defines the available debug configuration.
type Debug struct {
	Addr   string `yaml:"addr" env:"JOBENGINE_DEBUG_ADDR"`
	Token  string `yaml:"token" env:"JOBENGINE_DEBUG_TOKEN"`
	Pprof  bool   `yaml:"pprof" env:"JOBENGINE_DEBUG_PPROF"`
	Zpages bool   `yaml:"zpages" env:"JOBENGINE_DEBUG_ZPAGES"`
}

// HTTP defines the available http configuration.
type HTTP struct {
	Addr      string                `yaml:"addr" env:"JOBENGINE_HTTP_ADDR"`
	Namespace string                `yaml:"-"`
	Root      string                `yaml:"root" env:"JOBENGINE_HTTP_ROOT"`
	CORS      CORS                  `yaml:"cors"`
	TLS       shared.HTTPServiceTLS `yaml:"tls"`
}

// CORS defines the available cors configuration.
type CORS struct {
	AllowedOrigins   []string `yaml:"allow_origins" env:"OC_CORS_ALLOW_ORIGINS;JOBENGINE_CORS_ALLOW_ORIGINS"`
	AllowedMethods   []string `yaml:"allow_methods" env:"OC_CORS_ALLOW_METHODS;JOBENGINE_CORS_ALLOW_METHODS"`
	AllowedHeaders   []string `yaml:"allow_headers" env:"OC_CORS_ALLOW_HEADERS;JOBENGINE_CORS_ALLOW_HEADERS"`
	AllowCredentials bool     `yaml:"allow_credentials" env:"OC_CORS_ALLOW_CREDENTIALS;JOBENGINE_CORS_ALLOW_CREDENTIALS"`
}

// Events combines the configuration options for the event bus.
type Events struct {
	Endpoint             string `yaml:"endpoint" env:"OC_EVENTS_ENDPOINT;JOBENGINE_EVENTS_ENDPOINT"`
	Cluster              string `yaml:"cluster" env:"OC_EVENTS_CLUSTER;JOBENGINE_EVENTS_CLUSTER"`
	TLSInsecure          bool   `yaml:"tls_insecure" env:"OC_INSECURE;JOBENGINE_EVENTS_TLS_INSECURE"`
	TLSRootCACertificate string `yaml:"tls_root_ca_certificate" env:"OC_EVENTS_TLS_ROOT_CA_CERTIFICATE;JOBENGINE_EVENTS_TLS_ROOT_CA_CERTIFICATE"`
	EnableTLS            bool   `yaml:"enable_tls" env:"OC_EVENTS_ENABLE_TLS;JOBENGINE_EVENTS_ENABLE_TLS"`
	AuthUsername         string `yaml:"username" env:"OC_EVENTS_AUTH_USERNAME;JOBENGINE_EVENTS_AUTH_USERNAME"`
	AuthPassword         string `yaml:"password" env:"OC_EVENTS_AUTH_PASSWORD;JOBENGINE_EVENTS_AUTH_PASSWORD"`
}

// TokenManager is the config for using the reva token manager
type TokenManager struct {
	JWTSecret string `yaml:"jwt_secret" env:"OC_JWT_SECRET;JOBENGINE_JWT_SECRET"`
}
