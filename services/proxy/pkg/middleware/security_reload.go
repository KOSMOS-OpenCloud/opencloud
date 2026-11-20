package middleware

import (
	"net/http"
	"sync/atomic"

	"github.com/unrolled/secure"
	"github.com/unrolled/secure/cspbuilder"

	"github.com/opencloud-eu/opencloud/services/proxy/pkg/config"
)

// SecurityReloader applies the security relevant http headers like the Security middleware,
// the CSP configuration can be re-read at runtime.
type SecurityReloader struct {
	proxyCfg         *config.Config
	secureMiddleware atomic.Pointer[secure.Secure]
}

// NewSecurityReloader loads the CSP configuration and returns the reloader.
func NewSecurityReloader(proxyCfg *config.Config) (*SecurityReloader, error) {
	securityReloader := &SecurityReloader{proxyCfg: proxyCfg}
	if err := securityReloader.Reload(); err != nil {
		return nil, err
	}

	return securityReloader, nil
}

// Reload re-reads the CSP configuration files.
// A missing or broken file keeps the previous configuration active and returns the error.
func (s *SecurityReloader) Reload() error {
	cspConfig, err := LoadCSPConfig(s.proxyCfg)
	if err != nil {
		return err
	}

	cspBuilder := cspbuilder.Builder{
		Directives: cspConfig.Directives,
	}
	contentSecurityPolicy, err := cspBuilder.Build()
	if err != nil {
		return err
	}

	s.secureMiddleware.Store(newSecureMiddleware(contentSecurityPolicy))

	return nil
}

// Middleware returns the middleware, every request uses the configuration of the last successful Reload.
func (s *SecurityReloader) Middleware() func(h http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.secureMiddleware.Load().Handler(next).ServeHTTP(w, r)
		})
	}
}
