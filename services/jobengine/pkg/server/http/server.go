package http

import (
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/opencloud-eu/opencloud/pkg/account"
	"github.com/opencloud-eu/opencloud/pkg/cors"
	"github.com/opencloud-eu/opencloud/pkg/middleware"
	"github.com/opencloud-eu/opencloud/pkg/tracing"
	"github.com/riandyrn/otelchi"
)

// Server initializes a plain net/http server with chi router (no go-micro).
// This avoids go-micro's service registry which can lose the registration
// under sustained polling load, causing proxy 502 errors.
func Server(opts ...Option) (*stdhttp.Server, error) {
	options := newOptions(opts...)

	middlewares := []func(stdhttp.Handler) stdhttp.Handler{
		chimiddleware.RequestID,
		middleware.Version(
			options.Config.Service.Name,
			"dev",
		),
		middleware.Logger(
			options.Logger,
		),
		middleware.ExtractAccountUUID(
			account.Logger(options.Logger),
			account.JWTSecret(options.Config.TokenManager.JWTSecret),
		),
		middleware.Cors(
			cors.Logger(options.Logger),
			cors.AllowedOrigins(options.Config.HTTP.CORS.AllowedOrigins),
			cors.AllowedMethods(options.Config.HTTP.CORS.AllowedMethods),
			cors.AllowedHeaders(options.Config.HTTP.CORS.AllowedHeaders),
			cors.AllowCredentials(options.Config.HTTP.CORS.AllowCredentials),
		),
	}

	mux := chi.NewMux()
	mux.Use(middlewares...)

	mux.Use(
		otelchi.Middleware(
			"jobengine",
			otelchi.WithChiRoutes(mux),
			otelchi.WithTracerProvider(options.TraceProvider),
			otelchi.WithPropagators(tracing.GetPropagator()),
		),
	)

	// Register jobengine routes
	options.JobEngine.RegisterRoutes(mux)

	server := &stdhttp.Server{
		Addr:    options.Config.HTTP.Addr,
		Handler: mux,
	}

	return server, nil
}
