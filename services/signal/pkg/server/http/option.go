package http

import (
	"context"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/trace"

	relay "forge.mux.nu/xworx/opencraft/relay/signal"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/config"
)

// Option defines a single option function.
type Option func(o *Options)

// Options defines the available options for this package.
type Options struct {
	Logger        log.Logger
	Context       context.Context
	Config        *config.Config
	Flags         []pflag.Flag
	TraceProvider trace.TracerProvider
	Hub           *relay.Hub
}

// newOptions initializes the available default options.
func newOptions(opts ...Option) Options {
	opt := Options{}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

// Logger provides a function to set the logger option.
func Logger(val log.Logger) Option {
	return func(o *Options) { o.Logger = val }
}

// Context provides a function to set the context option.
func Context(val context.Context) Option {
	return func(o *Options) { o.Context = val }
}

// Config provides a function to set the config option.
func Config(val *config.Config) Option {
	return func(o *Options) { o.Config = val }
}

// Flags provides a function to set the flags option.
func Flags(flags ...pflag.Flag) Option {
	return func(o *Options) { o.Flags = append(o.Flags, flags...) }
}

// TraceProvider provides a function to set the TracerProvider option.
func TraceProvider(val trace.TracerProvider) Option {
	return func(o *Options) { o.TraceProvider = val }
}

// Hub provides a function to set the relay.
func Hub(val *relay.Hub) Option {
	return func(o *Options) { o.Hub = val }
}
