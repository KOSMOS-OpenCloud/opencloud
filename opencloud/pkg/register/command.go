package register

import (
	"github.com/opencloud-eu/opencloud/ocis-pkg/config"
	"github.com/urfave/cli/v2"
)

var (
	// Commands defines the slice of commands.
	Commands = []Command{}
)

// Command defines the register command.
type Command func(*config.Config) *cli.Command

// AddCommand appends a command to Commands.
func AddCommand(cmd Command) {
	Commands = append(
		Commands,
		cmd,
	)
}
