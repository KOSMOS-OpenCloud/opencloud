package runtime

import (
	"github.com/opencloud-eu/opencloud/pkg/log"
)

// Options is a runtime option
type Options struct {
	Services []string
	Logger   log.Logger
}

// Option undocumented
type Option func(o *Options)

// Services option
func Services(s []string) Option {
	return func(o *Options) {
		o.Services = append(o.Services, s...)
	}
}
