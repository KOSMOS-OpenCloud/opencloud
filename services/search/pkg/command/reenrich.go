package command

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config/parser"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ReEnrich re-processes files with missing metadata via Taki/LLM.
func ReEnrich(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "re-enrich",
		Short: "re-process files with missing metadata via Taki/LLM",
		Long: `Walks all files and calls Taki/LLM for metadata extraction.
By default, only MISSING metadata keys are written to xattrs.
Existing values (including user corrections) are never overwritten.

Use --force to overwrite ALL metadata keys (destructive!).
This requires interactive confirmation because user-corrected
metadata will be lost.

Bleve index is updated automatically via ArbitraryMetadataUpdated
events — no separate reindex needed after re-enrich.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			allSpacesFlag, _ := cmd.Flags().GetBool("all-spaces")
			spaceFlag, _ := cmd.Flags().GetString("space")
			endpointFlag, _ := cmd.Flags().GetString("endpoint")
			insecureFlag, _ := cmd.Flags().GetBool("insecure")
			forceFlag, _ := cmd.Flags().GetBool("force")

			if spaceFlag == "" && !allSpacesFlag {
				return errors.New("either --space or --all-spaces is required")
			}

			// --force requires interactive confirmation
			if forceFlag {
				fmt.Println("WARNING: --force will OVERWRITE all existing metadata.")
				fmt.Println("User corrections will be lost. This cannot be undone.")
				fmt.Print("Type 'yes' to continue: ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				if strings.TrimSpace(answer) != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			var dialOpts []grpc.DialOption
			if cfg.GRPCClientTLS.Mode == "insecure" || insecureFlag {
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			} else {
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS12,
				})))
			}

			conn, err := grpc.NewClient(endpointFlag, dialOpts...)
			if err != nil {
				return fmt.Errorf("failed to dial %s: %w", endpointFlag, err)
			}
			defer conn.Close()

			c := searchsvc.NewSearchProviderClient(conn)
			ctx := context.Background()

			if forceFlag {
				fmt.Println("Re-enriching: FORCE mode — all metadata will be overwritten")
			} else {
				fmt.Println("Re-enriching: only missing metadata keys will be written")
			}

			_, err = c.IndexSpace(ctx, &searchsvc.IndexSpaceRequest{
				SpaceId:        spaceFlag,
				ReEnrich:       true,
				ForceOverwrite: forceFlag,
			})
			if err != nil {
				fmt.Println("re-enrich failed: " + err.Error())
				return err
			}
			fmt.Println("Re-enrichment started. Monitor progress with: opencloud search status")
			return nil
		},
	}
	cmd.Flags().StringP("space", "s", "", "space ID to re-enrich. This or --all-spaces is required.")
	cmd.Flags().Bool("all-spaces", false, "re-enrich all spaces.")
	cmd.Flags().String("endpoint", "127.0.0.1:9220", "search service gRPC endpoint.")
	cmd.Flags().Bool("insecure", false, "disable TLS for gRPC.")
	cmd.Flags().Bool("force", false, "overwrite ALL metadata (destructive, requires confirmation).")

	return cmd
}
