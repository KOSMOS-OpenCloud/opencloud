package command

import (
	"os"

	"github.com/opencloud-eu/opencloud/pkg/clihelper"
	"github.com/opencloud-eu/opencloud/services/storage-shares/pkg/config"

	"github.com/spf13/cobra"
)

// GetCommands provides all commands for this service
func GetCommands(cfg *config.Config) []*cobra.Command {
	return []*cobra.Command{
		// start this service
		Server(cfg),

		// interaction with this service

		// infos about this service
		Health(cfg),
		Version(cfg),
	}
}

// Execute is the entry point for the storage-shares command.
func Execute(cfg *config.Config) error {
	app := clihelper.DefaultAppCobra(&cobra.Command{
		Use:   "storage-shares",
		Short: "Provide a virtual storage for shares in OpenCloud",
	})
	app.AddCommand(GetCommands(cfg)...)
	app.SetArgs(os.Args[1:])

	return app.ExecuteContext(cfg.Context)
}
