package command

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

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
// Uses force-rescan mode with the existing metadata protection in doUpsertItem —
// only missing keys are written, existing values are never overwritten.
func ReEnrich(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "re-enrich",
		Short: "re-process files with missing metadata via Taki/LLM",
		Long: `Triggers a force-rescan of the specified space(s). Each file is
re-extracted via Taki, but only MISSING metadata keys are written
to xattrs. Existing values (including manual corrections) are
never overwritten.

Use this after Taki/LLM was unavailable during a previous indexing
run, or after updating the Taki configuration.

The Taki /schema endpoint is used to determine which metadata keys
are expected per MIME type. Files that already have all expected
keys are still re-extracted (the metadata protection ensures no
data loss), but a future --dry-run mode could skip them entirely.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return configlog.ReturnFatal(parser.ParseConfig(cfg))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			allSpacesFlag, _ := cmd.Flags().GetBool("all-spaces")
			spaceFlag, _ := cmd.Flags().GetString("space")
			endpointFlag, _ := cmd.Flags().GetString("endpoint")
			insecureFlag, _ := cmd.Flags().GetBool("insecure")
			if spaceFlag == "" && !allSpacesFlag {
				return errors.New("either --space or --all-spaces is required")
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

			fmt.Println("Re-enriching: force-rescan with metadata protection (only missing keys will be written)")

			_, err = c.IndexSpace(ctx, &searchsvc.IndexSpaceRequest{
				SpaceId:      spaceFlag,
				ForceReindex: true, // force rescan to re-extract all files
			})
			if err != nil {
				fmt.Println("re-enrich failed: " + err.Error())
				return err
			}
			fmt.Println("Re-enrichment complete.")
			return nil
		},
	}
	cmd.Flags().StringP("space", "s", "", "space ID to re-enrich. This or --all-spaces is required.")
	cmd.Flags().Bool("all-spaces", false, "re-enrich all spaces.")
	cmd.Flags().String("endpoint", "127.0.0.1:9220", "search service gRPC endpoint.")
	cmd.Flags().Bool("insecure", false, "disable TLS for gRPC.")

	return cmd
}
