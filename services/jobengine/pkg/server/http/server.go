package http

import (
	"fmt"

	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/opencloud-eu/opencloud/pkg/account"
	"github.com/opencloud-eu/opencloud/pkg/cors"
	"github.com/opencloud-eu/opencloud/pkg/middleware"
	"github.com/opencloud-eu/opencloud/pkg/service/http"
	"github.com/opencloud-eu/opencloud/pkg/tracing"
	"github.com/opencloud-eu/opencloud/pkg/version"
	"github.com/riandyrn/otelchi"
	"go-micro.dev/v4"
)

// Server initializes the http service and server.
func Server(opts ...Option) (http.Service, error) {
	options := newOptions(opts...)

	service, err := http.NewService(
		http.TLSConfig(options.Config.HTTP.TLS),
		http.Logger(options.Logger),
		http.Namespace(options.Config.HTTP.Namespace),
		http.Name(options.Config.Service.Name),
		http.Version(version.GetString()),
		http.Address(options.Config.HTTP.Addr),
		http.Context(options.Context),
		http.Flags(options.Flags...),
		http.TraceProvider(options.TraceProvider),
	)
	if err != nil {
		return http.Service{}, fmt.Errorf("could not initialize http service: %w", err)
	}

	middlewares := []func(stdhttp.Handler) stdhttp.Handler{
		chimiddleware.RequestID,
		middleware.Version(
			options.Config.Service.Name,
			version.GetString(),
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

	if err := micro.RegisterHandler(service.Server(), mux); err != nil {
		return http.Service{}, err
	}

	return service, nil
}
