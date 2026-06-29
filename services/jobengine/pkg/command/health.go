package command

import (
	"context"
	"net"
	"net/http"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// Health is the entrypoint for the health command.
func Health(cfg *config.OCConfig) *cobra.Command {
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

func serveHTTP(ctx context.Context, addr string, handler http.Handler, logger zerolog.Logger) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	logger.Info().Str("addr", addr).Msg("http server listening")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
