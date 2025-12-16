package command

import (
	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/opencloud/pkg/runtime"
	"github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/pkg/config/parser"

	"github.com/spf13/cobra"
)

// Server is the entrypoint for the server command.
func Server(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "start a fullstack server (runtime and all services in supervised mode)",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnError(parser.ParseConfig(cfg, false))
		},
		GroupID: CommandGroupServer,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Prefer the in-memory registry as the default when running in single-binary mode
			r := runtime.New(cfg)
			return r.Start(cmd.Context())
		},
	}
}

func init() {
	register.AddCommand(Server)
}
