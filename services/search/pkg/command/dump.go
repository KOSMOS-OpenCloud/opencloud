package command

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve/v2"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config/parser"

	"github.com/spf13/cobra"
)

// Dump shows the contents of the search index for debugging.
func Dump(cfg *config.Config) *cobra.Command {
	dumpCmd := &cobra.Command{
		Use:   "dump",
		Short: "dump the search index contents for debugging",
		Long: `Shows all documents in the bleve search index, including their
stored fields and metadata. Use --query to filter by search term,
--limit to restrict output, and --fields to show only specific fields.

NOTE: The search service must be stopped before running this command,
as bleve locks the index directory.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			query, _ := cmd.Flags().GetString("query")
			limit, _ := cmd.Flags().GetInt("limit")
			fieldsFlag, _ := cmd.Flags().GetString("fields")
			jsonOutput, _ := cmd.Flags().GetBool("json")

			datapath := cfg.Engine.Bleve.Datapath
			if datapath == "" {
				return fmt.Errorf("bleve data path not configured")
			}

			indexPath := filepath.Join(datapath, "bleve")
			if _, err := os.Stat(indexPath); os.IsNotExist(err) {
				return fmt.Errorf("no index found at %s", indexPath)
			}

			index, err := bleve.Open(indexPath)
			if err != nil {
				return fmt.Errorf("failed to open index: %w", err)
			}
			defer index.Close()

			var req *bleve.SearchRequest
			if query != "" {
				req = bleve.NewSearchRequest(bleve.NewQueryStringQuery(query))
			} else {
				req = bleve.NewSearchRequest(bleve.NewMatchAllQuery())
			}
			if limit <= 0 {
				limit = math.MaxInt
			}
			req.Size = limit
			req.Fields = []string{"*"}

			res, err := index.Search(req)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			var fieldFilter map[string]bool
			if fieldsFlag != "" {
				fieldFilter = make(map[string]bool)
				for _, f := range strings.Split(fieldsFlag, ",") {
					fieldFilter[strings.TrimSpace(f)] = true
				}
			}

			fmt.Fprintf(os.Stderr, "Total documents: %d, showing: %d\n\n", res.Total, len(res.Hits))

			for i, hit := range res.Hits {
				if jsonOutput {
					filtered := hit.Fields
					if fieldFilter != nil {
						filtered = make(map[string]interface{})
						for k, v := range hit.Fields {
							prefix := strings.Split(k, ".")[0]
							if fieldFilter[k] || fieldFilter[prefix] {
								filtered[k] = v
							}
						}
					}
					b, _ := json.Marshal(map[string]interface{}{
						"id":     hit.ID,
						"score":  hit.Score,
						"fields": filtered,
					})
					fmt.Println(string(b))
				} else {
					fmt.Printf("--- [%d] %s (score: %.2f) ---\n", i+1, hit.ID, hit.Score)
					for k, v := range hit.Fields {
						if fieldFilter != nil {
							prefix := strings.Split(k, ".")[0]
							if !fieldFilter[k] && !fieldFilter[prefix] {
								continue
							}
						}
						fmt.Printf("  %-30s = %v\n", k, v)
					}
					fmt.Println()
				}
			}

			return nil
		},
	}
	dumpCmd.Flags().String("query", "", "filter by search query (bleve query syntax)")
	dumpCmd.Flags().Int("limit", 20, "maximum number of documents to show")
	dumpCmd.Flags().String("fields", "", "comma-separated list of fields to show (default: all)")
	dumpCmd.Flags().Bool("json", false, "output as JSON (one line per document)")

	return dumpCmd
}
