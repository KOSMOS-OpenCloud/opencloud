package debug

import (
	"net/http"

	"github.com/opencloud-eu/opencloud/pkg/service/debug"
	"github.com/opencloud-eu/opencloud/pkg/version"
)

// Server initializes the debug service and server.
func Server(opts ...Option) (*http.Server, error) {
	options := newOptions(opts...)

	return debug.NewService(
		debug.Logger(options.Logger),
		debug.Name(options.Config.Service.Name),
		debug.Version(version.GetString()),
		debug.Address(options.Config.Debug.Addr),
		debug.Token(options.Config.Debug.Token),
		debug.Pprof(options.Config.Debug.Pprof),
		debug.Zpages(options.Config.Debug.Zpages),
		//debug.CorsAllowedOrigins(options.Config.HTTP.CORS.AllowedOrigins),
		//debug.CorsAllowedMethods(options.Config.HTTP.CORS.AllowedMethods),
		//debug.CorsAllowedHeaders(options.Config.HTTP.CORS.AllowedHeaders),
		//debug.CorsAllowCredentials(options.Config.HTTP.CORS.AllowCredentials),
	), nil
}
