package middleware

import (
	"net/http"
	"os"

	gofig "github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/opencloud-eu/opencloud/services/proxy/pkg/config"
	"github.com/unrolled/secure"
	"github.com/unrolled/secure/cspbuilder"
)

// LoadCSPConfig loads CSP header configuration from a yaml file.
func LoadCSPConfig(proxyCfg *config.Config) (*config.CSP, error) {
	presetYamlContent, customYamlContent, err := loadCSPYaml(proxyCfg)
	if err != nil {
		return nil, err
	}
	return loadCSPConfig(presetYamlContent, customYamlContent)
}

// LoadCSPConfig loads CSP header configuration from a yaml file.
func loadCSPConfig(presetYamlContent, customYamlContent []byte) (*config.CSP, error) {
	// substitute env vars and load to struct
	gofig.WithOptions(gofig.ParseEnv)
	gofig.AddDriver(yaml.Driver)

	// TODO: merge all sources into one struct
	// ATM it is untested how this merger behaves with multiple sources
	// it might be better to load preset and custom separately and then merge structs
	// or load preset first and then custom to override values
	// especially in hindsight that there will be autoloaded config files from webapps
	// in the future
	err := gofig.LoadSources("yaml", presetYamlContent, customYamlContent)
	if err != nil {
		return nil, err
	}

	// read yaml
	cspConfig := config.CSP{}
	err = gofig.BindStruct("", &cspConfig)
	if err != nil {
		return nil, err
	}

	return &cspConfig, nil
}

func loadCSPYaml(proxyCfg *config.Config) ([]byte, []byte, error) {
	if proxyCfg.CSPConfigFileLocation == "" {
		return []byte(config.DefaultCSPConfig), nil, nil
	}
	customCSPYaml, err := os.ReadFile(proxyCfg.CSPConfigFileLocation)
	return []byte(config.DefaultCSPConfig), customCSPYaml, err
}

// Security is a middleware to apply security relevant http headers like CSP.
func Security(cspConfig *config.CSP) func(h http.Handler) http.Handler {
	cspBuilder := cspbuilder.Builder{
		Directives: cspConfig.Directives,
	}

	secureMiddleware := secure.New(secure.Options{
		BrowserXssFilter:             true,
		ContentSecurityPolicy:        cspBuilder.MustBuild(),
		ContentTypeNosniff:           true,
		CustomFrameOptionsValue:      "SAMEORIGIN",
		FrameDeny:                    true,
		ReferrerPolicy:               "strict-origin-when-cross-origin",
		STSSeconds:                   315360000,
		STSPreload:                   true,
		PermittedCrossDomainPolicies: "none",
		RobotTag:                     "none",
	})
	return func(next http.Handler) http.Handler {
		return secureMiddleware.Handler(next)
	}
}
