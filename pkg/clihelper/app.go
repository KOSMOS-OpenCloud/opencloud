package clihelper

import (
	"fmt"

	"github.com/opencloud-eu/opencloud/pkg/version"

	"github.com/spf13/cobra"
)

// DefaultApp is a wrapper for DefaultApp that adds Cobra specific settings
func DefaultApp(app *cobra.Command) *cobra.Command {
	// TODO: when migration is done this has to become DefaultApp
	// version info
	app.Version = fmt.Sprintf("%s (%s <%s>) (%s)", version.String, "OpenCloud GmbH", "support@opencloud.eu", version.Compiled())

	return app
}
