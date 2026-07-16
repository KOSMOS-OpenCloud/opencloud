package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config/parser"

	"github.com/spf13/cobra"
)

// Test probes all search subsystems and reports their status.
func Test(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "test all search subsystems (taki, LLM, embedding, whisper, qdrant)",
		Long: `Probes every subsystem in the search/enrichment pipeline and
reports which ones are working and which are failing. Use this
to diagnose why search results are missing metadata, embeddings,
or full-text content.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Test Taki /test endpoint
			takiURL := cfg.Extractor.Tika.TikaURL
			if takiURL == "" {
				fmt.Println("Taki:           DISABLED (no extractor configured)")
				return nil
			}

			fmt.Printf("Taki:           %s\n", takiURL)

			resp, err := http.Get(strings.TrimSuffix(takiURL, "/") + "/../test")
			if err != nil {
				// Try without /../ (tika URL might be http://tika:9998 not http://tika:9998/rmeta/text)
				resp, err = http.Get(strings.TrimSuffix(strings.TrimSuffix(takiURL, "/rmeta/text"), "/tika/text") + "/test")
			}
			if err != nil {
				fmt.Printf("  Connection:   FAILED — %v\n", err)
				return nil
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			var result struct {
				Status     string `json:"status"`
				Version    string `json:"version"`
				Subsystems []struct {
					Name    string `json:"name"`
					Status  string `json:"status"`
					Detail  string `json:"detail"`
					Latency string `json:"latency"`
				} `json:"subsystems"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				fmt.Printf("  Response:     INVALID — %s\n", string(body)[:200])
				return nil
			}

			fmt.Printf("  Version:      %s\n", result.Version)
			fmt.Printf("  Overall:      %s\n", strings.ToUpper(result.Status))

			for _, sub := range result.Subsystems {
				icon := "✓"
				if sub.Status == "failed" {
					icon = "✗"
				} else if sub.Status == "disabled" {
					icon = "–"
				}
				latency := ""
				if sub.Latency != "" {
					latency = " (" + sub.Latency + ")"
				}
				fmt.Printf("  %s %-12s %s%s\n", icon, sub.Name+":", strings.ToUpper(sub.Status), latency)
				if sub.Detail != "" && sub.Status != "ok" {
					fmt.Printf("    → %s\n", sub.Detail)
				}
			}

			// 2. Test Qdrant
			fmt.Println()
			if cfg.Vector.URL != "" {
				qdrantURL := strings.TrimSuffix(cfg.Vector.URL, "/")
				collection := cfg.Vector.Collection
				if collection == "" {
					collection = "opencloud"
				}
				resp, err := http.Get(qdrantURL + "/collections/" + collection)
				if err != nil {
					fmt.Printf("Qdrant:         FAILED — %v\n", err)
				} else {
					defer resp.Body.Close()
					var qResult struct {
						Result struct {
							PointsCount  int `json:"points_count"`
							VectorsCount int `json:"vectors_count"`
						} `json:"result"`
					}
					json.NewDecoder(resp.Body).Decode(&qResult)
					fmt.Printf("Qdrant:         OK (%s)\n", qdrantURL)
					fmt.Printf("  Collection:   %s\n", collection)
					fmt.Printf("  Points:       %d\n", qResult.Result.PointsCount)
					fmt.Printf("  Vectors:      %d\n", qResult.Result.VectorsCount)
					if qResult.Result.VectorsCount == 0 && qResult.Result.PointsCount > 0 {
						fmt.Printf("  ⚠ WARNING:    points without vectors — embedding backend may be offline\n")
					}
				}
			} else {
				fmt.Println("Qdrant:         DISABLED")
			}

			return nil
		},
	}
}
