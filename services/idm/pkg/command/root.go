package command

import (
	"os"

	"github.com/opencloud-eu/opencloud/pkg/clihelper"
	"github.com/opencloud-eu/opencloud/services/idm/pkg/config"
	"github.com/urfave/cli/v2"
)

// GetCommands provides all commands for this service
func GetCommands(cfg *config.Config) cli.Commands {
	return []*cli.Command{
		// start this service
		Server(cfg),

		// interaction with this service
		ResetPassword(cfg),

		// infos about this service
		Health(cfg),
		Version(cfg),
	}
}

// Execute is the entry point for the opencloud idm command.
func Execute(cfg *config.Config) error {
	app := clihelper.DefaultApp(&cli.App{
		Name:     "idm",
		Usage:    "Embedded LDAP service for OpenCloud",
		Commands: GetCommands(cfg),
	})

	return app.RunContext(cfg.Context, os.Args)
}
