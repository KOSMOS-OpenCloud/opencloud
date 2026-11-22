package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"

	"go-micro.dev/v4"

	relay "github.com/flash7777/craft-signal-relay/signal"

	"github.com/opencloud-eu/opencloud/pkg/account"
	"github.com/opencloud-eu/opencloud/pkg/middleware"
	"github.com/opencloud-eu/opencloud/pkg/service/http"
	"github.com/opencloud-eu/opencloud/pkg/version"
)

// Handler is everything the service answers: the relay's routes behind the account middleware, which reads
// the user from the reva token the proxy sets. The two WebSocket routes come without a token (a browser can't
// set a header there); their first frame carries the ticket. No middleware here wraps the ResponseWriter: a
// WebSocket needs the connection itself.
func Handler(hub *relay.Hub, jwtSecret string, accountOptions ...account.Option) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	hub.Routes(mux)
	mux.HandleFunc("GET /signal/health", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "sessions": hub.Sessions()})
	})
	return middleware.ExtractAccountUUID(append(accountOptions, account.JWTSecret(jwtSecret))...)(mux)
}

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

	handler := Handler(options.Hub, options.Config.TokenManager.JWTSecret, account.Logger(options.Logger))
	if err := micro.RegisterHandler(service.Server(), handler); err != nil {
		return http.Service{}, err
	}

	return service, nil
}
