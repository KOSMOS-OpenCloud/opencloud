package command

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/pkg/clihelper"
	"github.com/opencloud-eu/opencloud/pkg/config"

	"github.com/spf13/cobra"
)

// Execute is the entry point for the opencloud command.
func Execute() error {
	cfg := config.DefaultConfig()

	app := clihelper.DefaultApp(&cobra.Command{
		Use:   "opencloud",
		Short: "opencloud",
	})

	for _, fn := range register.Commands {
		cmd := fn(cfg)
		// if the command has a RunE function, it is a subcommand of root
		// if not it is a consumed root command from the services
		// wee need to overwrite the RunE function to get the help output there
		if cmd.RunE == nil {
			cmd.RunE = func(cmd *cobra.Command, args []string) error {
				cmd.Help()
				return nil
			}
		}
		app.AddCommand(cmd)
	}
	app.SetArgs(os.Args[1:])
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	return app.ExecuteContext(ctx)
}
