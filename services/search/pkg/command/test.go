package command

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config/parser"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
							PointsCount         int `json:"points_count"`
							IndexedVectorsCount int `json:"indexed_vectors_count"`
						} `json:"result"`
					}
					json.NewDecoder(resp.Body).Decode(&qResult)
					fmt.Printf("Qdrant:         OK (%s)\n", qdrantURL)
					fmt.Printf("  Collection:   %s\n", collection)
					fmt.Printf("  Points:       %d\n", qResult.Result.PointsCount)
					fmt.Printf("  Vectors:      %d\n", qResult.Result.IndexedVectorsCount)
					if qResult.Result.IndexedVectorsCount == 0 && qResult.Result.PointsCount > 0 {
						fmt.Printf("  ⚠ WARNING:    points without vectors — embedding backend may be offline\n")
					}
				}
			} else {
				fmt.Println("Qdrant:         DISABLED")
			}

			// 3. Test Search gRPC endpoint (re-enrich readiness)
			fmt.Println()
			grpcEndpoint := "127.0.0.1:9220"
			var dialOpts []grpc.DialOption
			if cfg.GRPCClientTLS.Mode == "insecure" {
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			} else {
				// Try insecure first, fall back to TLS
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, err := grpc.DialContext(ctx, grpcEndpoint, append(dialOpts, grpc.WithBlock())...)
			if err != nil {
				// Retry with TLS
				dialOpts = []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}))}
				ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel2()
				conn, err = grpc.DialContext(ctx2, grpcEndpoint, append(dialOpts, grpc.WithBlock())...)
			}
			if err != nil {
				fmt.Printf("Search gRPC:    FAILED — %v\n", err)
			} else {
				defer conn.Close()
				// Quick ping via IndexSpace with empty ID (will fail but proves connectivity)
				c := searchsvc.NewSearchProviderClient(conn)
				_, pingErr := c.IndexSpace(ctx, &searchsvc.IndexSpaceRequest{SpaceId: "__ping__"})
				if pingErr != nil && strings.Contains(pingErr.Error(), "Unavailable") {
					fmt.Printf("Search gRPC:    FAILED — %v\n", pingErr)
				} else {
					fmt.Printf("Search gRPC:    OK (%s)\n", grpcEndpoint)
				}
			}

			// 4. Indexing status (via debug HTTP endpoint)
			fmt.Println()
			statusResp, err := http.Get(fmt.Sprintf("http://%s/index-status", cfg.Debug.Addr))
			if err == nil {
				defer statusResp.Body.Close()
				var idxStatus struct {
					Running        bool   `json:"running"`
					SpaceCurrent   int    `json:"space_current"`
					SpaceTotal     int    `json:"space_total"`
					SpaceID        string `json:"space_id"`
					FilesProcessed int64  `json:"files_processed"`
					Errors         int    `json:"errors"`
					StartedAt      string `json:"started_at"`
					FinishedAt     string `json:"finished_at"`
				}
				if err := json.NewDecoder(statusResp.Body).Decode(&idxStatus); err == nil {
					if idxStatus.Running {
						started, _ := time.Parse(time.RFC3339Nano, idxStatus.StartedAt)
						elapsed := time.Since(started).Truncate(time.Second)
						var rate float64
						if elapsed.Seconds() > 0 {
							rate = float64(idxStatus.FilesProcessed) / elapsed.Seconds()
						}
						fmt.Printf("Indexing:       RUNNING (since %s)\n", elapsed)
						if idxStatus.SpaceTotal > 0 {
							fmt.Printf("  Space:        %d/%d\n", idxStatus.SpaceCurrent, idxStatus.SpaceTotal)
						}
						fmt.Printf("  Files:        %d processed (%.1f/s)\n", idxStatus.FilesProcessed, rate)
						if idxStatus.Errors > 0 {
							if idxStatus.FilesProcessed > 0 {
								pct := float64(idxStatus.Errors) / float64(idxStatus.FilesProcessed) * 100
								fmt.Printf("  Errors:       %d (%.2f%%)\n", idxStatus.Errors, pct)
							} else {
								fmt.Printf("  Errors:       %d\n", idxStatus.Errors)
							}
						}
					} else if idxStatus.FinishedAt != "" && idxStatus.FinishedAt != "0001-01-01T00:00:00Z" {
						started, _ := time.Parse(time.RFC3339Nano, idxStatus.StartedAt)
						finished, _ := time.Parse(time.RFC3339Nano, idxStatus.FinishedAt)
						duration := finished.Sub(started).Truncate(time.Second)
						var rate float64
						if duration.Seconds() > 0 {
							rate = float64(idxStatus.FilesProcessed) / duration.Seconds()
						}
						fmt.Printf("Indexing:       IDLE\n")
						fmt.Printf("  Last run:     %s (duration: %s)\n", idxStatus.FinishedAt[:19], duration)
						fmt.Printf("  Files:        %d processed (%.1f/s)\n", idxStatus.FilesProcessed, rate)
						if idxStatus.Errors > 0 {
							if idxStatus.FilesProcessed > 0 {
								pct := float64(idxStatus.Errors) / float64(idxStatus.FilesProcessed) * 100
								fmt.Printf("  Errors:       %d (%.2f%%)\n", idxStatus.Errors, pct)
							} else {
								fmt.Printf("  Errors:       %d\n", idxStatus.Errors)
							}
						}
					} else {
						fmt.Printf("Indexing:       no run recorded\n")
					}
				}
			}

			return nil
		},
	}
}
