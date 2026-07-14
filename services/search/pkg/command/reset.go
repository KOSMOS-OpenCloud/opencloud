package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config/parser"

	"github.com/spf13/cobra"
)

// Reset deletes the search index so it gets recreated with the current mapping on next start.
func Reset(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "delete the search index (recreated on next start/reindex)",
		Long: `Deletes the bleve search index directory. The index will be
recreated with the current mapping when the search service starts
or when 'opencloud search index' is run.

Use this after changing the index mapping (e.g. adding StoreDynamic)
to ensure the new mapping takes effect.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			datapath := cfg.Engine.Bleve.Datapath
			if datapath == "" {
				return fmt.Errorf("bleve data path not configured")
			}

			indexPath := filepath.Join(datapath, "bleve")

			info, err := os.Stat(indexPath)
			if os.IsNotExist(err) {
				fmt.Printf("No index found at %s — nothing to reset.\n", indexPath)
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to stat index path: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", indexPath)
			}

			if err := os.RemoveAll(indexPath); err != nil {
				return fmt.Errorf("failed to delete index: %w", err)
			}

			fmt.Printf("Search index deleted: %s\n", indexPath)
			fmt.Println("Run 'opencloud search index --all-spaces --insecure' to rebuild.")
			return nil
		},
	}
}
