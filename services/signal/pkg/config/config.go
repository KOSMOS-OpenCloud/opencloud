package config

import (
	"context"

	"github.com/opencloud-eu/opencloud/pkg/shared"
)

// Config combines all available configuration parts.
type Config struct {
	Commons *shared.Commons `yaml:"-"`

	Service Service `yaml:"-"`

	LogLevel string `yaml:"loglevel" env:"OC_LOG_LEVEL;SIGNAL_LOG_LEVEL"`

	Debug Debug `yaml:"debug"`
	HTTP  HTTP  `yaml:"http"`

	TokenManager  *TokenManager         `yaml:"token_manager"`
	GRPCClientTLS *shared.GRPCClientTLS `yaml:"grpc_client_tls"`
	RevaGateway   string                `yaml:"reva_gateway" env:"OC_REVA_GATEWAY"`

	Context context.Context `yaml:"-"`

	// BlobBudget is the session buffer of the relay for large content (pictures, pasted blocks) in bytes.
	BlobBudget int `yaml:"blob_budget" env:"SIGNAL_BLOB_BUDGET"`
	// Operators are the ids of the users who see every session in full (GET /signal/sessions). Empty: nobody.
	Operators []string `yaml:"operators" env:"SIGNAL_OPERATORS"`
	// HiddenUsers are the ids of the users who may join a session unseen (a recorder, for debugging).
	// Empty: switched off on this instance.
	HiddenUsers []string `yaml:"hidden_users" env:"SIGNAL_HIDDEN_USERS"`
}

// Service defines the service name.
type Service struct {
	Name string `yaml:"-"`
}

// Debug defines the available debug configuration.
type Debug struct {
	Addr   string `yaml:"addr" env:"SIGNAL_DEBUG_ADDR"`
	Token  string `yaml:"token" env:"SIGNAL_DEBUG_TOKEN"`
	Pprof  bool   `yaml:"pprof" env:"SIGNAL_DEBUG_PPROF"`
	Zpages bool   `yaml:"zpages" env:"SIGNAL_DEBUG_ZPAGES"`
}

// HTTP defines the available http configuration.
type HTTP struct {
	Addr      string                `yaml:"addr" env:"SIGNAL_HTTP_ADDR"`
	Namespace string                `yaml:"-"`
	TLS       shared.HTTPServiceTLS `yaml:"tls"`
}

// TokenManager is the config for using the reva token manager
type TokenManager struct {
	JWTSecret string `yaml:"jwt_secret" env:"OC_JWT_SECRET;SIGNAL_JWT_SECRET"`
}
