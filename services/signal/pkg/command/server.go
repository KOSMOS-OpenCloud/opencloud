package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/spf13/cobra"

	relay "forge.mux.nu/xworx/opencraft/relay/signal"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/pkg/registry"
	"github.com/opencloud-eu/opencloud/pkg/runner"
	"github.com/opencloud-eu/opencloud/pkg/tracing"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/config"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/config/parser"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/server/debug"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/server/http"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/service"
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

			tlsMode, err := pool.StringToTLSMode(cfg.GRPCClientTLS.Mode)
			if err != nil {
				return err
			}
			gatewaySelector, err := pool.GatewaySelector(cfg.RevaGateway,
				pool.WithTLSCACert(cfg.GRPCClientTLS.CACert),
				pool.WithTLSMode(tlsMode),
				pool.WithRegistry(registry.GetRegistry()),
				pool.WithTracerProvider(tracerProvider),
			)
			if err != nil {
				return err
			}

			// The relay: it hands frames on and never reads what the apps send each other. Who may join which
			// document it asks OpenCloud (service.Hooks).
			hooks := service.Hooks{
				Logger:      logger,
				Stat:        service.GatewayStat(gatewaySelector),
				Operators:   cfg.Operators,
				HiddenUsers: cfg.HiddenUsers,
			}
			options := hooks.Options(cfg.BlobBudget)
			options.Log = slog.New(slog.DiscardHandler)
			hub := relay.New(options)

			logger.Info().Str("addr", cfg.HTTP.Addr).Bool("hidden", len(cfg.HiddenUsers) > 0).Msg("signal relay starting")

			{
				httpServer, err := http.Server(
					http.Logger(logger),
					http.Config(cfg),
					http.Context(ctx),
					http.TraceProvider(tracerProvider),
					http.Hub(hub),
				)
				if err != nil {
					logger.Error().Err(err).Str("transport", "http").Msg("Failed to initialize server")
					return err
				}
				gr.Add(runner.NewGoMicroHttpServerRunner(cfg.Service.Name+".http", httpServer))
			}

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
