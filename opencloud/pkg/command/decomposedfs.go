package command

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/pkg/config"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposed/blobstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/store"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// DecomposedfsCommand is the entrypoint for the groups command.
func DecomposedfsCommand(cfg *config.Config) *cobra.Command {
	decomposedCmd := &cobra.Command{
		Use:     "decomposedfs",
		Short:   `cli tools to inspect and manipulate a decomposedfs storage.`,
		GroupID: CommandGroupStorage,
	}
	decomposedCmd.AddCommand(metadataCmd(cfg), checkCmd(cfg))
	return decomposedCmd
}

func init() {
	register.AddCommand(DecomposedfsCommand)
}

func checkCmd(cfg *config.Config) *cobra.Command {
	cCmd := &cobra.Command{
		Use:   "check-treesize",
		Short: `cli tool to check the treesize metadata of a Space`,
		RunE:  check,
	}
	cCmd.Flags().StringP("root", "r", "", "Path to the root directory of the decomposedfs")
	err := cCmd.MarkFlagRequired("root")
	if err != nil {
		fmt.Println(err)
	}
	cCmd.Flags().StringP("node", "n", "", "Space ID of the Space to inspect")
	err = cCmd.MarkFlagRequired("node")
	if err != nil {
		fmt.Println(err)
	}
	cCmd.Flags().Bool("repair", false, "Try to repair nodes with incorrect treesize metadata. IMPORTANT: Only use this while OpenCloud is not running.")
	cCmd.Flags().Bool("force", false, "Do not prompt for confirmation when running in repair mode.")

	return cCmd
}

func check(cmd *cobra.Command, args []string) error {
	rootFlag := cmd.Flag("root").Value.String()
	repairFlag, _ := cmd.Flags().GetBool("repair")
	forceFlag, _ := cmd.Flags().GetBool("force")

	if repairFlag && !forceFlag {
		answer := strings.ToLower(stringPrompt("IMPORTANT: Only use '--repair' when OpenCloud is not running. Do you want to continue? [yes | no = default]"))
		if answer != "yes" && answer != "y" {
			return nil
		}
	}

	lu, backend := getBackend(cmd)
	o := &options.Options{
		MetadataBackend: backend.Name(),
		MaxConcurrency:  100,
	}
	bs, err := blobstore.New(rootFlag)
	if err != nil {
		fmt.Println("Failed to init blobstore")
		return err
	}

	tree := tree.New(lu, bs, o, permissions.Permissions{}, store.Create(), &zerolog.Logger{})

	nId := cmd.Flag("node").Value.String()
	n, err := lu.NodeFromSpaceID(context.Background(), nId)
	if err != nil || !n.Exists {
		fmt.Println("Can not find node '" + nId + "'")
		return err
	}
	fmt.Printf("Checking treesizes in space: %s (id: %s)\n", n.Name, n.ID)
	ctx := revactx.ContextSetUser(context.Background(),
		&userpb.User{
			Id: &userpb.UserId{
				OpaqueId: "00000000-0000-0000-0000-000000000000",
			},
			Username: "offline",
		})

	treeSize, err := walkTree(ctx, tree, lu, n, repairFlag)
	treesizeFromMetadata, err := n.GetTreeSize(cmd.Context())
	if err != nil {
		fmt.Printf("failed to read treesize of node: %s: %s\n", n.ID, err)
	}
	if treesizeFromMetadata != treeSize {
		fmt.Printf("Tree sizes mismatch for space: %s\n\tNodeId: %s\n\tInternalPath: %s\n\tcalculated treesize: %d\n\ttreesize in metadata: %d\n",
			n.Name, n.ID, n.InternalPath(), treeSize, treesizeFromMetadata)
		if repairFlag {
			fmt.Printf("Fixing tree size for node: %s. Calculated treesize: %d\n",
				n.ID, treeSize)
			n.SetTreeSize(cmd.Context(), treeSize)
		}
	}
	return nil
}

func walkTree(ctx context.Context, tree *tree.Tree, lu *lookup.Lookup, root *node.Node, repair bool) (uint64, error) {
	if root.Type(ctx) != provider.ResourceType_RESOURCE_TYPE_CONTAINER {
		return 0, errors.New("can't travers non-container nodes")
	}
	children, err := tree.ListFolder(ctx, root)
	if err != nil {
		fmt.Println("Can not list children for space'" + root.ID + "'")
		return 0, err
	}

	var treesize uint64
	for _, child := range children {
		switch child.Type(ctx) {
		case provider.ResourceType_RESOURCE_TYPE_CONTAINER:
			subtreesize, err := walkTree(ctx, tree, lu, child, repair)
			if err != nil {
				fmt.Printf("error calculating tree size of node: %s: %s\n", child.ID, err)
				return 0, err
			}
			treesizeFromMetadata, err := child.GetTreeSize(ctx)
			if err != nil {
				fmt.Printf("failed to read tree size of node: %s: %s\n", child.ID, err)
				return 0, err
			}
			if treesizeFromMetadata != subtreesize {
				origin, err := lu.Path(ctx, child, node.NoCheck)
				if err != nil {
					fmt.Printf("error get path: %s\n", err)
				}
				fmt.Printf("Tree sizes mismatch for node: %s\n\tNodeId: %s\n\tInternalPath: %s\n\tcalculated treesize: %d\n\ttreesize in metadata: %d\n",
					origin, child.ID, child.InternalPath(), subtreesize, treesizeFromMetadata)
				if repair {
					fmt.Printf("Fixing tree size for node: %s. Calculated treesize: %d\n",
						child.ID, subtreesize)
					child.SetTreeSize(ctx, subtreesize)
				}
			}
			treesize += subtreesize
		case provider.ResourceType_RESOURCE_TYPE_FILE:
			blobsize, err := child.GetBlobSize(ctx)
			if err != nil {
				fmt.Printf("error reading blobsize of node: %s: %s\n", child.ID, err)
				return 0, err
			}
			treesize += blobsize
		default:
			fmt.Printf("Ignoring type: %v, node: %s %s\n", child.Type(ctx), child.Name, child.ID)
		}
	}

	return treesize, nil
}

func metadataCmd(cfg *config.Config) *cobra.Command {
	metaCmd := &cobra.Command{
		Use:   "metadata",
		Short: `cli tools to inspect and manipulate node metadata`,
	}
	metaCmd.AddCommand(dumpCmd(cfg), getCmd(cfg), setCmd(cfg))
	metaCmd.Flags().StringP("root", "r", "", "Path to the root directory of the decomposedfs")
	err := metaCmd.MarkFlagRequired("root")
	if err != nil {
		fmt.Println(err)
	}
	metaCmd.Flags().StringP("node", "n", "", "Path to or ID of the node to inspect")
	err = metaCmd.MarkFlagRequired("node")
	if err != nil {
		fmt.Println(err)
	}
	return metaCmd
}

func dumpCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: `print the metadata of the given node. String attributes will be enclosed in quotes. Binary attributes will be returned encoded as base64 with their value being prefixed with '0s'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			lu, backend := getBackend(cmd)
			path, err := getNode(cmd, lu)
			if err != nil {
				return err
			}

			attribs, err := backend.All(cmd.Context(), path)
			if err != nil {
				fmt.Println("Error reading attributes")
				return err
			}
			printAttribs(attribs, cmd.Flag("attribute").Value.String())
			return nil
		},
	}
}

func getCmd(cfg *config.Config) *cobra.Command {
	gCmd := &cobra.Command{
		Use:   "get",
		Short: `print a specific attribute of the given node. String attributes will be enclosed in quotes. Binary attributes will be returned encoded as base64 with their value being prefixed with '0s'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			lu, backend := getBackend(cmd)
			path, err := getNode(cmd, lu)
			if err != nil {
				return err
			}

			attribs, err := backend.All(cmd.Context(), path)
			if err != nil {
				fmt.Println("Error reading attributes")
				return err
			}
			printAttribs(attribs, cmd.Flag("attribute").Value.String())
			return nil
		},
	}
	gCmd.Flags().StringP("attribute", "a", "", "attribute to inspect, can be a glob pattern (e.g. 'user.*' will match all attributes starting with 'user.').")
	return gCmd
}

func setCmd(cfg *config.Config) *cobra.Command {
	sCmd := &cobra.Command{
		Use:   "set",
		Short: `manipulate metadata of the given node. Binary attributes can be given hex encoded (prefix by '0x') or base64 encoded (prefix by '0s').`,
		RunE: func(cmd *cobra.Command, args []string) error {
			lu, backend := getBackend(cmd)
			n, err := getNode(cmd, lu)
			if err != nil {
				return err
			}

			v := cmd.Flag("value").Value.String()
			if strings.HasPrefix(v, "0s") {
				b64, err := base64.StdEncoding.DecodeString(v[2:])
				if err == nil {
					v = string(b64)
				} else {
					fmt.Printf("Error decoding base64 string: '%s'. Using as raw string.\n", err)
				}
			} else if strings.HasPrefix(v, "0x") {
				h, err := hex.DecodeString(v[2:])
				if err == nil {
					v = string(h)
				} else {
					fmt.Printf("Error decoding base64 string: '%s'. Using as raw string.\n", err)
				}
			}

			err = backend.Set(cmd.Context(), n, cmd.Flag("attribute").Value.String(), []byte(v))
			if err != nil {
				fmt.Println("Error setting attribute")
				return err
			}
			return nil
		},
	}
	sCmd.Flags().StringP("attribute", "a", "", "attribute to inspect, can be a glob pattern (e.g. 'user.*' will match all attributes starting with 'user.').")
	err := sCmd.MarkFlagRequired("attribute")
	if err != nil {
		fmt.Println(err)
	}

	sCmd.Flags().StringP("value", "v", "", "value to set")
	err = sCmd.MarkFlagRequired("value")
	if err != nil {
		fmt.Println(err)
	}

	return sCmd
}

func backend(root, backend string) metadata.Backend {
	switch backend {
	case "xattrs":
		return metadata.NewXattrsBackend(cache.Config{})
	case "mpk":
		return metadata.NewMessagePackBackend(cache.Config{})
	}
	return metadata.NullBackend{}
}

func getBackend(cmd *cobra.Command) (*lookup.Lookup, metadata.Backend) {
	rootFlag := cmd.Flag("root").Value.String()

	bod := lookup.DetectBackendOnDisk(rootFlag)
	backend := backend(rootFlag, bod)
	lu := lookup.New(backend, &options.Options{
		Root:            rootFlag,
		MetadataBackend: bod,
	}, &timemanager.Manager{})
	return lu, backend
}

func getNode(cmd *cobra.Command, lu *lookup.Lookup) (*node.Node, error) {
	nodeFlag := cmd.Flag("node").Value.String()

	id, err := storagespace.ParseID(nodeFlag)
	if err != nil {
		fmt.Println("Invalid node id.")
		return nil, err
	}
	return lu.NodeFromID(context.Background(), &id)
}

func printAttribs(attribs map[string][]byte, onlyAttribute string) {
	if onlyAttribute != "" {
		fmt.Println(onlyAttribute + `=` + attribToString(attribs[onlyAttribute]))
		return
	}

	names := []string{}
	for k := range attribs {
		names = append(names, k)
	}

	sort.Strings(names)

	for _, n := range names {
		fmt.Println(n + `=` + attribToString(attribs[n]))
	}
}

func attribToString(attrib []byte) string {
	for i := 0; i < len(attrib); i++ {
		if attrib[i] < 32 || attrib[i] >= 127 {
			return "0s" + base64.StdEncoding.EncodeToString(attrib)
		}
	}
	return `"` + string(attrib) + `"`
}
