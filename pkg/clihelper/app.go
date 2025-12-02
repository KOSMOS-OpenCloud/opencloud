package clihelper

import (
	"fmt"

	"github.com/opencloud-eu/opencloud/pkg/version"
	"github.com/spf13/cobra"
	"github.com/urfave/cli/v2"
)

// DefaultApp provides some default settings for the cli app
func DefaultApp(app *cli.App) *cli.App {
	// version info
	app.Version = version.String
	app.Compiled = version.Compiled()

	// author info
	app.Authors = []*cli.Author{
		{
			Name:  "OpenCloud GmbH",
			Email: "support@opencloud.eu",
		},
	}

	// disable global version flag
	// instead we provide the version command
	app.HideVersion = true

	return app
}

// DefaultAppCobra is a wrapper for DefaultApp that adds Cobra specific settings
func DefaultAppCobra(app *cobra.Command) *cobra.Command {
	// TODO: when migration is done this has to become DefaultApp
	// version info
	app.Version = fmt.Sprintf("%s (%s <%s>) (%s)", version.String, "OpenCloud GmbH", "support@opencloud.eu", version.Compiled())

	return app
}
