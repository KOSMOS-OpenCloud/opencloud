package runtime

import (
	"context"

	"github.com/opencloud-eu/opencloud/opencloud/pkg/runtime/service"
	"github.com/opencloud-eu/opencloud/pkg/config"
)

// Runtime represents an oCIS runtime environment.
type Runtime struct {
	c *config.Config
}

// New creates a new oCIS + micro runtime
func New(cfg *config.Config) Runtime {
	return Runtime{
		c: cfg,
	}
}

// Start rpc runtime
func (r *Runtime) Start(ctx context.Context) error {
	return service.Start(ctx, service.WithConfig(r.c))
}
