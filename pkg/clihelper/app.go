package clihelper

import (
	"github.com/opencloud-eu/opencloud/pkg/version"
	"github.com/urfave/cli/v2"
)

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
