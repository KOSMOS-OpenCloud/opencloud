package command

import (
	"os"

	"github.com/opencloud-eu/opencloud/pkg/clihelper"
	"github.com/opencloud-eu/opencloud/services/antivirus/pkg/config"
	"github.com/spf13/cobra"
)

// GetCommands provides all commands for this service
func GetCommands(cfg *config.Config) []*cobra.Command {
	return []*cobra.Command{
		Server(cfg),
		Health(cfg),
		Version(cfg),
	}
}

// Execute is the entry point for the antivirus command.
func Execute(cfg *config.Config) error {
	app := clihelper.DefaultAppCobra(&cobra.Command{
		Use:   "antivirus",
		Short: "Antivirus service for OpenCloud",
	})
	app.AddCommand(GetCommands(cfg)...)
	app.SetArgs(os.Args[1:])
	return app.ExecuteContext(cfg.Context)
}
