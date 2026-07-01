package command

import (
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	"github.com/spf13/cobra"
)

// Health is the entrypoint for the health command.
func Health(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "check health status",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := log.Configure(cfg.Service.Name, cfg.Commons, cfg.LogLevel)
			logger.Debug().Msg("checking health")
			return nil
		},
	}
}
