package registry

import (
	"context"
	"net/http"
	"time"

	mRegistry "go-micro.dev/v4/registry"

	"github.com/opencloud-eu/opencloud/pkg/log"
)

// RegisterService publishes an arbitrary endpoint to the service-registry. This allows querying nodes of
// non-micro services like reva. No health-checks are done, thus the caller is responsible for canceling.
func RegisterService(ctx context.Context, logger log.Logger, service *mRegistry.Service, debugAddr string) error {
	registry := GetRegistry()
	node := service.Nodes[0]

	logger.Info().Msgf("registering external service %v@%v", node.Id, node.Address)

	rOpts := []mRegistry.RegisterOption{mRegistry.RegisterTTL(GetRegisterTTL())}

	// A failing initial registration must not kill the process (it used to
	// logger.Fatal here, taking down every service in the container while
	// JetStream was not write-ready — incident 2026-08-20). Retry a few
	// times, then hand over to the periodic re-registration below.
	retryDelay := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		err := registry.Register(service, rOpts...)
		if err == nil {
			break
		}
		logger.Error().Err(err).Msgf("registration error for external service %v (attempt %d/3)", service.Name, attempt)
		if attempt == 3 {
			break
		}
		time.Sleep(retryDelay)
		retryDelay *= 2
	}

	t := time.NewTicker(GetRegisterInterval())

	go func() {
		// check if the service is ready
		delay := 500 * time.Millisecond
		for {
			resp, err := http.DefaultClient.Get("http://" + debugAddr + "/readyz")
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				break
			}
			time.Sleep(delay)
			delay *= 2
		}
		for {
			select {
			case <-t.C:
				logger.Debug().Interface("service", service).Msg("refreshing external service-registration")
				err := registry.Register(service, rOpts...)
				if err != nil {
					logger.Error().Err(err).Msgf("registration error for external service %v", service.Name)
				}
			case <-ctx.Done():
				logger.Debug().Interface("service", service).Msg("unregistering")
				t.Stop()
				err := registry.Deregister(service)
				if err != nil {
					logger.Err(err).Msgf("Error unregistering external service %v", service.Name)
				}
				return
			}
		}
	}()

	return nil
}
