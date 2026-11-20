package http

import (
	"fmt"
	"path"
	"sync/atomic"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"go-micro.dev/v4"

	"github.com/opencloud-eu/opencloud/pkg/cors"
	"github.com/opencloud-eu/opencloud/pkg/middleware"
	"github.com/opencloud-eu/opencloud/pkg/registry"
	"github.com/opencloud-eu/opencloud/pkg/reload"
	"github.com/opencloud-eu/opencloud/pkg/service/http"
	"github.com/opencloud-eu/opencloud/pkg/version"
	"github.com/opencloud-eu/opencloud/pkg/x/io/fsx"
	"github.com/opencloud-eu/opencloud/services/web"
	"github.com/opencloud-eu/opencloud/services/web/pkg/apps"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config/parser"
	svc "github.com/opencloud-eu/opencloud/services/web/pkg/service/v0"
)

var (
	// _customAppsEndpoint path is used to make app artifacts available by the web service.
	_customAppsEndpoint = "/assets/apps"
)

// Server initializes the http service and server.
func Server(opts ...Option) (http.Service, error) {
	options := newOptions(opts...)

	service, err := http.NewService(
		http.TLSConfig(options.Config.HTTP.TLS),
		http.Logger(options.Logger),
		http.Namespace(options.Namespace),
		http.Name("web"),
		http.Version(version.GetString()),
		http.Address(options.Config.HTTP.Addr),
		http.Context(options.Context),
		http.Flags(options.Flags...),
		http.TraceProvider(options.TraceProvider),
	)
	if err != nil {
		options.Logger.Error().
			Err(err).
			Msg("Error initializing http service")
		return http.Service{}, fmt.Errorf("could not initialize http service: %w", err)
	}

	gatewaySelector, err := pool.GatewaySelector(
		options.Config.GatewayAddress,
		pool.WithRegistry(registry.GetRegistry()),
		pool.WithTracerProvider(options.TraceProvider),
	)
	if err != nil {
		return http.Service{}, err
	}

	appsFS := fsx.NewFallbackFS(
		fsx.NewReadOnlyFs(fsx.NewBasePathFs(fsx.NewOsFs(), options.Config.Asset.AppsPath)),
		fsx.NewBasePathFs(fsx.FromIOFS(web.Assets), "assets/apps"),
	)
	// build the list of applications, it is rebuilt when apps.yaml or the application files change
	appsReloader := apps.NewReloader(
		options.Logger,
		options.Config.Apps,
		path.Join(options.Config.HTTP.Root, _customAppsEndpoint),
		options.Config.Web.Config.ExternalApps,
		appsFS.Secondary().IOFS(), appsFS.Primary().IOFS(),
	)
	// the web configuration is kept exchangeable, SIGHUP re-reads it
	var currentWeb atomic.Pointer[config.Web]
	initialWeb := options.Config.Web
	currentWeb.Store(&initialWeb)
	options.Config.Web.Config.ExternalApps = appsReloader.ExternalApps()

	reload.Register("web.apps", appsReloader.Reload)
	reload.Register("web.config", func() error {
		reloadedWeb, err := parser.ReloadWeb(options.Config)
		if err != nil {
			return err
		}
		currentWeb.Store(&reloadedWeb)
		appsReloader.SetBaseApps(reloadedWeb.Config.ExternalApps)
		return nil
	})

	coreFS := fsx.NewFallbackFS(
		fsx.NewBasePathFs(fsx.NewOsFs(), options.Config.Asset.CorePath),
		fsx.NewBasePathFs(fsx.FromIOFS(web.Assets), "assets/core"),
	)
	themeFS := fsx.NewFallbackFS(
		fsx.NewBasePathFs(fsx.NewOsFs(), options.Config.Asset.ThemesPath),
		fsx.NewBasePathFs(fsx.FromIOFS(web.Assets), "assets/themes"),
	)

	handle, err := svc.NewService(
		svc.Logger(options.Logger),
		svc.CoreFS(coreFS.IOFS()),
		svc.AppFS(appsFS.IOFS()),
		svc.ThemeFS(themeFS),
		svc.AppsHTTPEndpoint(_customAppsEndpoint),
		svc.Config(options.Config),
		svc.GatewaySelector(gatewaySelector),
		svc.ExternalApps(appsReloader.ExternalApps),
		svc.WebConfig(func() config.Web { return *currentWeb.Load() }),
		svc.Middleware(
			chimiddleware.RealIP,
			chimiddleware.RequestID,
			chimiddleware.Compress(5),
			middleware.NoCache,
			middleware.Version(
				options.Config.Service.Name,
				version.GetString(),
			),
			middleware.Logger(
				options.Logger,
			),
			middleware.Cors(
				cors.Logger(options.Logger),
				cors.AllowedOrigins(options.Config.HTTP.CORS.AllowedOrigins),
				cors.AllowedMethods(options.Config.HTTP.CORS.AllowedMethods),
				cors.AllowedHeaders(options.Config.HTTP.CORS.AllowedHeaders),
				cors.AllowCredentials(options.Config.HTTP.CORS.AllowCredentials),
			),
		),
		svc.TraceProvider(options.TraceProvider),
	)

	if err != nil {
		return http.Service{}, err
	}

	{
		handle = svc.NewInstrument(handle, options.Metrics)
		handle = svc.NewLogging(handle, options.Logger)
	}

	if err := micro.RegisterHandler(service.Server(), handle); err != nil {
		return http.Service{}, err
	}

	return service, nil
}
