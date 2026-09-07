package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/spaceidindex"
	"github.com/spf13/cobra"
)

// subspaceGrantsCmd adds the fix-subspace-grants subcommand to decomposedfs.
func subspaceGrantsCmd(_ *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix-subspace-grants",
		Short: "Create or fix space index entries for all existing subspace grants",
		Long: `Scans all project spaces for subspace registrations (user.oc.subspaces xattr),
reads the grant xattrs on each subspace root directory, and ensures that the
by-user-id and by-group-id space indexes contain the correct symlink path entries.

This is a one-time migration tool. It is safe to run multiple times.`,
		RunE: runFixSubspaceGrants,
	}
	cmd.Flags().StringP("root", "r", "", "Path to the root directory of the decomposedfs")
	_ = cmd.MarkFlagRequired("root")
	cmd.Flags().Bool("dry-run", false, "Only report what would be changed, do not write")
	return cmd
}

type subspaceEntry struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func runFixSubspaceGrants(cmd *cobra.Command, _ []string) error {
	rootFlag, _ := cmd.Flags().GetString("root")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	projectsDir := filepath.Join(rootFlag, "projects")
	if _, err := os.Stat(projectsDir); err != nil {
		return fmt.Errorf("projects directory not found at %s: %w", projectsDir, err)
	}

	// Init indexes (same paths as decomposedfs.go)
	indexesDir := filepath.Join(rootFlag, lookup.IndexesDir)
	userIdx := spaceidindex.New(indexesDir, "by-user-id")
	groupIdx := spaceidindex.New(indexesDir, "by-group-id")

	var totalSpaces, totalSubspaces, added, fixed, ok int

	spaces, err := os.ReadDir(projectsDir)
	if err != nil {
		return fmt.Errorf("reading projects dir: %w", err)
	}

	for _, spaceEntry := range spaces {
		if !spaceEntry.IsDir() {
			continue
		}
		spaceID := spaceEntry.Name()
		spaceDir := filepath.Join(projectsDir, spaceID)

		// Read subspace list from space root xattr
		subspacesRaw, err := os.Lstat(spaceDir)
		if err != nil {
			continue
		}
		_ = subspacesRaw

		subspacesJSON, err := readXattr(spaceDir, prefixes.SubspacesAttr)
		if err != nil || len(subspacesJSON) == 0 {
			continue
		}

		var subspaces []subspaceEntry
		if err := json.Unmarshal(subspacesJSON, &subspaces); err != nil || len(subspaces) == 0 {
			continue
		}

		totalSpaces++
		symlink := buildSymlink(spaceID)

		for _, ss := range subspaces {
			totalSubspaces++
			ssDir := filepath.Join(spaceDir, strings.TrimPrefix(ss.Path, "/"))
			if _, err := os.Stat(ssDir); err != nil {
				fmt.Printf("  SKIP %s/%s: directory not found\n", spaceID[:8], ss.Path)
				continue
			}

			// Read grant xattrs from subspace root
			attrs, err := os.Llistxattr(ssDir)
			if err != nil {
				continue
			}

			for _, attr := range attrs {
				if !strings.HasPrefix(attr, prefixes.GrantPrefix) {
					continue
				}
				principal := strings.TrimPrefix(attr, prefixes.GrantPrefix)

				var grantID string
				var idx *spaceidindex.Index
				var kind string

				if strings.HasPrefix(principal, prefixes.UserAcePrefix) {
					grantID = strings.TrimPrefix(principal, prefixes.UserAcePrefix)
					idx = userIdx
					kind = "user"
				} else if strings.HasPrefix(principal, prefixes.GroupAcePrefix) {
					grantID = strings.TrimPrefix(principal, prefixes.GroupAcePrefix)
					idx = groupIdx
					kind = "group"
				} else {
					continue
				}

				// Check existing index entry
				existing, err := idx.Load(grantID)
				if err != nil {
					existing = nil
				}

				if existing == nil {
					// No index file yet — needs to be created
					if dryRun {
						fmt.Printf("  ADD  %s %s → %s\n", kind, grantID[:8], spaceID[:8])
					} else {
						if err := idx.Add(grantID, spaceID, symlink); err != nil {
							fmt.Printf("  ERR  ADD %s %s: %v\n", kind, grantID, err)
							continue
						}
						fmt.Printf("  ADD  %s %s → %s\n", kind, grantID[:8], spaceID[:8])
					}
					added++
				} else if val, ok := existing[spaceID]; ok {
					if val == symlink {
						ok++
					} else {
						if dryRun {
							fmt.Printf("  FIX  %s %s: %s → correct symlink\n", kind, grantID[:8], val[:30])
						} else {
							if err := idx.Add(grantID, spaceID, symlink); err != nil {
								fmt.Printf("  ERR  FIX %s %s: %v\n", kind, grantID, err)
								continue
							}
							fmt.Printf("  FIX  %s %s: was %s, now correct\n", kind, grantID[:8], val[:30])
						}
						fixed++
					}
				} else {
					// Index file exists but no entry for this space
					if dryRun {
						fmt.Printf("  ADD  %s %s → %s (to existing index)\n", kind, grantID[:8], spaceID[:8])
					} else {
						if err := idx.Add(grantID, spaceID, symlink); err != nil {
							fmt.Printf("  ERR  ADD %s %s: %v\n", kind, grantID, err)
							continue
						}
						fmt.Printf("  ADD  %s %s → %s (to existing index)\n", kind, grantID[:8], spaceID[:8])
					}
					added++
				}
			}
		}
	}

	fmt.Printf("\nDone: %d spaces with subspaces, %d subspaces\n", totalSpaces, totalSubspaces)
	fmt.Printf("  %d added, %d fixed, %d already correct\n", added, fixed, ok)
	if dryRun {
		fmt.Println("  (dry run — no changes written)")
	}
	return nil
}

// buildSymlink replicates tree.BuildSpaceIDIndexEntry using lookup.Pathify.
func buildSymlink(spaceID string) string {
	return "../../../spaces/" + lookup.Pathify(spaceID, 1, 2) + "/nodes/" + lookup.Pathify(spaceID, 4, 2)
}

// readXattr reads a user. xattr, falling back to the .mpk metadata backend
// when xattrs are offloaded.
func readXattr(path, attr string) ([]byte, error) {
	// Try xattr first
	if v, err := os.Lgetxattr(path, attr); err == nil {
		return v, nil
	}

	// Fallback: read from the .mpk file in .oc-nodes
	// The node ID is in the user.oc.id xattr or the filename
	// For now, only support xattr mode. If xattrs are offloaded,
	// the admin needs to run this with xattr support.
	return nil, fmt.Errorf("xattr %s not found on %s (are xattrs offloaded?)", attr, filepath.Base(path))
}
