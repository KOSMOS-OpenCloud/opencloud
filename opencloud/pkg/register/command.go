package register

import (
	"github.com/spf13/cobra"

	"github.com/opencloud-eu/opencloud/pkg/config"
)

var (
	// Commands define the slice of commands.
	Commands []Command
)

// Command defines the register command.
type Command func(*config.Config) *cobra.Command

// AddCommand appends a command to Commands.
func AddCommand(cmd Command) {
	Commands = append(
		Commands,
		cmd,
	)
}
