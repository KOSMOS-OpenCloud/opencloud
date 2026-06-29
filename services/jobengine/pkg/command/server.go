package command

import (
	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/spf13/cobra"

	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	svc "github.com/opencloud-eu/opencloud/services/jobengine/pkg/service"
)

// Server is the entrypoint for the server command.
func Server(cfg *config.OCConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: fmt.Sprintf("start the %s service", cfg.Service.Name),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := log.Configure(cfg.Service.Name, cfg.Commons, cfg.LogLevel)

			// Load pipeline config
			engineCfg := config.Defaults()
			engineCfg.Service.MaxWorkers = cfg.MaxWorkers
			engineCfg.Service.QueueSize = cfg.QueueSize
			engineCfg.Service.TempDir = cfg.TempDir
			engineCfg.Service.PipelineDirs = cfg.PipelineDirs
			engineCfg.Service.ListenAddr = cfg.HTTP.Addr

			if cfg.ConfigFile != "" {
				loaded, err := config.Load(cfg.ConfigFile)
				if err != nil {
					logger.Warn().Err(err).Str("file", cfg.ConfigFile).Msg("could not load config file, using defaults")
				} else {
					engineCfg = loaded
				}
			} else {
				// Scan pipeline dirs even without config file
				for _, dir := range cfg.PipelineDirs {
					engineCfg.LoadPipelineDir(dir)
				}
			}

			engine := svc.New(engineCfg)
			defer engine.Shutdown()

			r := chi.NewRouter()
			engine.RegisterRoutes(r)

			logger.Info().
				Int("pipelines", len(engineCfg.Pipelines)).
				Int("workers", engineCfg.Service.MaxWorkers).
				Str("addr", cfg.HTTP.Addr).
				Msg("jobengine starting")

			for id, p := range engineCfg.Pipelines {
				logger.Info().Str("id", id).Str("label", p.Label).Str("type", p.Executor.Type).Msg("pipeline registered")
			}

			// Block on HTTP server
			return serveHTTP(cmd.Context(), cfg.HTTP.Addr, r, logger)
		},
	}
}
