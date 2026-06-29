package command

import (
	"os"

	"github.com/opencloud-eu/opencloud/pkg/clihelper"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	"github.com/spf13/cobra"
)

// GetCommands provides all commands for this service
func GetCommands(cfg *config.OCConfig) []*cobra.Command {
	return []*cobra.Command{
		Server(cfg),
		Health(cfg),
	}
}

// Execute is the entry point for the jobengine command.
func Execute(cfg *config.OCConfig) error {
	app := clihelper.DefaultApp(&cobra.Command{
		Use:   "jobengine",
		Short: "starts jobengine service",
	})
	app.AddCommand(GetCommands(cfg)...)
	app.SetArgs(os.Args[1:])
	return app.ExecuteContext(cfg.Context)
}
