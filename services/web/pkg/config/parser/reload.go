package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	"gopkg.in/yaml.v3"

	occfg "github.com/opencloud-eu/opencloud/pkg/config"
	ocdefaults "github.com/opencloud-eu/opencloud/pkg/config/defaults"
	"github.com/opencloud-eu/opencloud/pkg/config/envdecode"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config/defaults"
)

// ReloadWeb re-reads the web part of the configuration (the content of config.json, theme server and theme path)
// the same way the start does: defaults, web section of opencloud.yaml, web.yaml, environment, json config file.
// The running configuration is not changed, a broken file returns an error.
func ReloadWeb(runningCfg *config.Config) (config.Web, error) {
	reloadedCfg := defaults.DefaultConfig()
	reloadedCfg.Commons = runningCfg.Commons

	// the config binding ignores syntax errors, a broken file must not silently reset the configuration to its defaults
	for _, configName := range []string{"opencloud", runningCfg.Service.Name} {
		if err := checkYamlSyntax(configName); err != nil {
			return config.Web{}, err
		}
	}

	openCloudCfg := struct {
		Web *config.Config `yaml:"web"`
	}{Web: reloadedCfg}
	if err := occfg.BindSourcesToStructs("opencloud", &openCloudCfg); err != nil {
		return config.Web{}, err
	}
	if err := occfg.BindSourcesToStructs(runningCfg.Service.Name, reloadedCfg); err != nil {
		return config.Web{}, err
	}

	defaults.EnsureDefaults(reloadedCfg)

	if err := envdecode.Decode(reloadedCfg); err != nil {
		// no environment variable set for this config is an expected "error"
		if !errors.Is(err, envdecode.ErrNoTargetFieldsAreSet) {
			return config.Web{}, err
		}
	}

	if reloadedCfg.File != "" {
		jsonContent, err := os.ReadFile(reloadedCfg.File)
		if err != nil {
			return config.Web{}, err
		}
		if err := json.Unmarshal(jsonContent, &reloadedCfg.Web.Config); err != nil {
			return config.Web{}, fmt.Errorf("%s: %w", reloadedCfg.File, err)
		}
	}

	defaults.Sanitize(reloadedCfg)

	return reloadedCfg.Web, nil
}

// checkYamlSyntax reports a syntax error of the given config file, a missing file is valid.
func checkYamlSyntax(configName string) error {
	configFile := path.Join(ocdefaults.BaseConfigPath(), configName+".yaml")

	yamlContent, err := os.ReadFile(configFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}

	var syntaxCheck map[string]any
	if err := yaml.Unmarshal(yamlContent, &syntaxCheck); err != nil {
		return fmt.Errorf("%s: %w", configFile, err)
	}

	return nil
}
