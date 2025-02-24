package config

import (
	"github.com/opencloud-eu/opencloud/pkg/shared"
)

// HTTP defines the available http configuration.
type HTTP struct {
	Addr      string                `yaml:"addr" env:"COLLABORATION_HTTP_ADDR" desc:"The bind address of the HTTP service." introductionVersion:"1.0.0"`
	Namespace string                `yaml:"-"`
	TLS       shared.HTTPServiceTLS `yaml:"tls"`
}
