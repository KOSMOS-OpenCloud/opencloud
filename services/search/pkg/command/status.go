package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config/parser"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"

	"github.com/spf13/cobra"
)

// Status queries the indexing status from the running search service.
func Status(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show indexing status",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(
				fmt.Sprintf("http://%s/index-status", cfg.Debug.Addr),
			)
			if err != nil {
				return fmt.Errorf("failed to query status: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}

			var status search.IndexStatus
			if err := json.Unmarshal(body, &status); err != nil {
				return fmt.Errorf("failed to parse status: %w", err)
			}

			if status.Running {
				fmt.Printf("Indexing: RUNNING\n")
				if status.SpaceTotal > 0 {
					fmt.Printf("  Space:  %d/%d\n", status.SpaceCurrent, status.SpaceTotal)
				}
				fmt.Printf("  ID:     %s\n", status.SpaceID)
				fmt.Printf("  Files:  %d processed\n", status.FilesProcessed)
				fmt.Printf("  Since:  %s\n", status.StartedAt.Format("15:04:05"))
			} else if !status.FinishedAt.IsZero() {
				fmt.Printf("Indexing: IDLE\n")
				fmt.Printf("  Last:   %s\n", status.FinishedAt.Format("2006-01-02 15:04:05"))
				fmt.Printf("  Files:  %d processed\n", status.FilesProcessed)
				if status.Errors > 0 {
					fmt.Printf("  Errors: %d\n", status.Errors)
				}
			} else {
				fmt.Printf("Indexing: no run recorded\n")
			}

			return nil
		},
	}
}
