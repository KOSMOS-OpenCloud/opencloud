package parser

import (
	"encoding/json"
	"errors"

	occfg "github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/opencloud/pkg/shared"
	"github.com/opencloud-eu/opencloud/services/idp/pkg/backends/directories"
	"github.com/opencloud-eu/opencloud/services/idp/pkg/config"
	"github.com/opencloud-eu/opencloud/services/idp/pkg/config/defaults"

	"github.com/opencloud-eu/opencloud/pkg/config/envdecode"
)

// ParseConfig loads configuration from known paths.
func ParseConfig(cfg *config.Config) error {
	err := occfg.BindSourcesToStructs(cfg.Service.Name, cfg)
	if err != nil {
		return err
	}

	defaults.EnsureDefaults(cfg)

	// load all env variables relevant to the config in the current context.
	if err := envdecode.Decode(cfg); err != nil {
		// no environment variable set for this config is an expected "error"
		if !errors.Is(err, envdecode.ErrNoTargetFieldsAreSet) {
			return err
		}
	}

	defaults.Sanitize(cfg)

	return Validate(cfg)
}

func Validate(cfg *config.Config) error {
	switch cfg.IDP.IdentityManager {
	case "cs3":
		if cfg.MachineAuthAPIKey == "" {
			return shared.MissingMachineAuthApiKeyError(cfg.Service.Name)
		}
	case "ldap":
		if cfg.Ldap.BindPassword == "" {
			return shared.MissingLDAPBindPassword(cfg.Service.Name)
		}
	case "directories":
		// The 'directories' identity manager keeps the internal idm as the primary
		// directory for all users and only delegates the password check for users
		// explicitly assigned to an external directory. It uses the same idm bind as
		// the 'ldap' manager, so the same bind password is required.
		if cfg.Ldap.BindPassword == "" {
			return shared.MissingLDAPBindPassword(cfg.Service.Name)
		}
		// Fail fast on a malformed directory list. An empty list is valid: every user
		// then stays internal, behaving identically to the 'ldap' manager. The check
		// reuses the exact rules the backend applies at runtime, so a broken config is
		// detected at deployment time instead of by a user hitting a broken login.
		var dirs []shared.Directory
		if cfg.Commons != nil {
			dirs = cfg.Commons.Directories
		}
		blob, err := json.Marshal(dirs)
		if err != nil {
			return err
		}
		if _, err := directories.ParseDirectories(string(blob)); err != nil {
			return err
		}
	}

	return nil
}
