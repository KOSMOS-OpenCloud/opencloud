package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/pkg/runner"
	"github.com/opencloud-eu/opencloud/pkg/tracing"
	"github.com/opencloud-eu/opencloud/pkg/version"
	pipeconfig "codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/config"
	pipeengine "codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/engine"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config/parser"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/metrics"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/server/debug"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/server/http"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/service"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/events/stream"
	"github.com/opencloud-eu/opencloud/pkg/generators"
	"github.com/spf13/cobra"
)

// Server is the entrypoint for the server command.
func Server(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: fmt.Sprintf("start the %s service without runtime (unsupervised mode)", cfg.Service.Name),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := log.Configure(cfg.Service.Name, cfg.Commons, cfg.LogLevel)
			tracerProvider, err := tracing.GetTraceProvider(cmd.Context(), cfg.Commons.TracesExporter, cfg.Service.Name)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to initialize tracer")
				return err
			}

			gr := runner.NewGroup()
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			mtrcs := metrics.New()
			mtrcs.BuildInfo.WithLabelValues(version.GetString()).Set(1)

			// Load pipeline configuration
			engineCfg := pipeconfig.PipelineDefaults()
			engineCfg.Service.MaxWorkers = cfg.MaxWorkers
			engineCfg.Service.QueueSize = cfg.QueueSize
			engineCfg.Service.TempDir = cfg.TempDir
			engineCfg.Service.PipelineDirs = cfg.PipelineDirs

			if cfg.ConfigFile != "" {
				loaded, err := pipeconfig.LoadPipelineConfig(cfg.ConfigFile)
				if err != nil {
					logger.Warn().Err(err).Str("file", cfg.ConfigFile).Msg("could not load pipeline config, using defaults")
				} else {
					engineCfg = loaded
				}
			} else {
				for _, dir := range cfg.PipelineDirs {
					engineCfg.LoadPipelineDir(dir)
				}
			}

			engine := pipeengine.New(engineCfg, &service.RevaAuthExtractor{})
			defer engine.Shutdown()

			// Connect to NATS for SSE notifications (optional)
			if cfg.Events.Endpoint != "" {
			connName := generators.GenerateConnectionName(cfg.Service.Name, generators.NTypeBus)
			natsStream, err := stream.NatsFromConfig(connName, false, stream.NatsConfig{
				Endpoint:             cfg.Events.Endpoint,
				Cluster:              cfg.Events.Cluster,
				TLSInsecure:          cfg.Events.TLSInsecure,
				TLSRootCACertificate: cfg.Events.TLSRootCACertificate,
				EnableTLS:            cfg.Events.EnableTLS,
				AuthUsername:         cfg.Events.AuthUsername,
				AuthPassword:         cfg.Events.AuthPassword,
			})
			if err != nil {
				logger.Warn().Err(err).Msg("NATS not available, job notifications disabled")
			} else {
				engine.OnJobDone = func(job *pipeengine.Job) {
					// Check pipeline notification setting
					if p, ok := engineCfg.Pipelines[job.Pipeline]; ok && p.Notification == "none" {
						return
					}
					b, err := json.Marshal(job)
					if err != nil {
						logger.Error().Err(err).Msg("could not marshal job for SSE")
						return
					}
					if err := events.Publish(context.Background(), natsStream, events.SendSSE{
						UserIDs: []string{job.UserID},
						Type:    "job-finished",
						Message: b,
					}); err != nil {
						logger.Error().Err(err).Msg("could not publish job SSE event")
					}
				}
			}
			} else {
				logger.Info().Msg("NATS not configured, job SSE notifications disabled")
			}

			// Load pipe matrix
			if cfg.MatrixFile != "" {
				if err := engine.LoadMatrix(cfg.MatrixFile); err != nil {
					logger.Warn().Err(err).Str("file", cfg.MatrixFile).Msg("could not load pipe matrix")
				} else {
					logger.Info().Str("file", cfg.MatrixFile).Msg("pipe matrix loaded")
				}
			}

			logger.Info().
				Int("pipelines", len(engineCfg.Pipelines)).
				Int("workers", engineCfg.Service.MaxWorkers).
				Str("addr", cfg.HTTP.Addr).
				Msg("jobengine starting")

			for id, p := range engineCfg.Pipelines {
				logger.Info().Str("id", id).Str("label", p.Label).Str("jobType", p.Job.Type).Msg("pipeline registered")
			}

			// HTTP Server
			{
				httpServer, err := http.Server(
					http.Logger(logger),
					http.Config(cfg),
					http.Context(ctx),
					http.TraceProvider(tracerProvider),
					http.JobEngine(engine),
				)
				if err != nil {
					logger.Error().Err(err).Str("transport", "http").Msg("Failed to initialize server")
					return err
				}

				gr.Add(runner.NewGoMicroHttpServerRunner(cfg.Service.Name+".http", httpServer))
			}

			// Debug Server
			{
				debugServer, err := debug.Server(
					debug.Logger(logger),
					debug.Context(ctx),
					debug.Config(cfg),
				)
				if err != nil {
					logger.Error().Err(err).Str("server", "debug").Msg("Failed to initialize server")
					return err
				}

				gr.Add(runner.NewGolangHttpServerRunner(cfg.Service.Name+".debug", debugServer))
			}

			grResults := gr.Run(ctx)
			for _, grResult := range grResults {
				if grResult.RunnerError != nil {
					return grResult.RunnerError
				}
			}
			return nil
		},
	}
}
