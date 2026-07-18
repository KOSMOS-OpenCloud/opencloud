package http

import (
	"context"

	"github.com/opencloud-eu/opencloud/pkg/log"
	pipeengine "codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/engine"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"

	"go.opentelemetry.io/otel/trace"
)

// Option defines a single option function.
type Option func(o *Options)

// Options defines the available options for this package.
type Options struct {
	Logger        log.Logger
	Context       context.Context
	Config        *config.Config
	TraceProvider trace.TracerProvider
	JobEngine     *pipeengine.JobEngine
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

// TraceProvider provides a function to set the TracerProvider option.
func TraceProvider(val trace.TracerProvider) Option {
	return func(o *Options) { o.TraceProvider = val }
}

// JobEngine provides a function to set the JobEngine option.
func JobEngine(val *pipeengine.JobEngine) Option {
	return func(o *Options) { o.JobEngine = val }
}
