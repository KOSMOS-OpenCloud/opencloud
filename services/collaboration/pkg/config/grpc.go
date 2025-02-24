package config

// GRPC defines the available grpc configuration.
type GRPC struct {
	Addr     string `yaml:"addr" env:"COLLABORATION_GRPC_ADDR" desc:"The bind address of the GRPC service." introductionVersion:"1.0.0"`
	Protocol string `yaml:"protocol" env:"OC_GRPC_PROTOCOL;COLLABORATION_GRPC_PROTOCOL" desc:"The transport protocol of the GRPC service." introductionVersion:"1.0.0"`

	Namespace string `yaml:"-"`
}
