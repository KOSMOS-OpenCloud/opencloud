package command

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/opencloud-eu/opencloud/ocis-pkg/clihelper"
	"github.com/opencloud-eu/opencloud/ocis-pkg/config"
	"github.com/opencloud-eu/opencloud/ocis/pkg/register"
	"github.com/urfave/cli/v2"
)

// Execute is the entry point for the ocis command.
func Execute() error {
	cfg := config.DefaultConfig()

	app := clihelper.DefaultApp(&cli.App{
		Name:  "ocis",
		Usage: "ownCloud Infinite Scale",
	})

	for _, fn := range register.Commands {
		app.Commands = append(
			app.Commands,
			fn(cfg),
		)
	}

	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	return app.RunContext(ctx, os.Args)
}
