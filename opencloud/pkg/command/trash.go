package command

import (
	"fmt"

	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/opencloud/pkg/trash"
	"github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/pkg/config/parser"

	"github.com/spf13/cobra"
)

func TrashCommand(cfg *config.Config) *cobra.Command {
	trashCmd := &cobra.Command{
		Use:   "trash",
		Short: "OpenCloud trash functionality",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnError(parser.ParseConfig(cfg, true))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Read the docs")
			return nil
		},
	}
	trashCmd.AddCommand(TrashPurgeEmptyDirsCommand(cfg))

	return trashCmd
}

func TrashPurgeEmptyDirsCommand(cfg *config.Config) *cobra.Command {
	trashPurgeCmd := &cobra.Command{
		Use:   "purge-empty-dirs",
		Short: "purge empty directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			basePath := cmd.Flag("basepath").Value.String()
			if basePath == "" {
				fmt.Println("basepath is required")
				_ = cmd.Help()
				return nil
			}

			if err := trash.PurgeTrashEmptyPaths(basePath, cmd.Flag("dry-run").Changed); err != nil {
				fmt.Println(err)
				return err
			}

			return nil
		},
	}
	trashPurgeCmd.Flags().StringP("basepath", "p", "", "the basepath of the decomposedfs (e.g. /var/tmp/opencloud/storage/users)")
	err := trashPurgeCmd.MarkFlagRequired("basepath")
	if err != nil {
		fmt.Println(err)
	}

	trashPurgeCmd.Flags().Bool("dry-run", true, "do not delete anything, just print what would be deleted")

	return trashPurgeCmd
}

func init() {
	register.AddCommand(TrashCommand)
}
