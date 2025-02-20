package command

import (
	"errors"
	"fmt"

	"github.com/opencloud-eu/opencloud/opencloud/pkg/backup"
	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/pkg/config/parser"
	decomposedbs "github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposed/blobstore"
	decomposeds3bs "github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposeds3/blobstore"
	"github.com/urfave/cli/v2"
)

// BackupCommand is the entrypoint for the backup command
func BackupCommand(cfg *config.Config) *cli.Command {
	return &cli.Command{
		Name:  "backup",
		Usage: "OpenCloud backup functionality",
		Subcommands: []*cli.Command{
			ConsistencyCommand(cfg),
		},
		Before: func(c *cli.Context) error {
			return configlog.ReturnError(parser.ParseConfig(cfg, true))
		},
		Action: func(_ *cli.Context) error {
			fmt.Println("Read the docs")
			return nil
		},
	}
}

// ConsistencyCommand is the entrypoint for the consistency Command
func ConsistencyCommand(cfg *config.Config) *cli.Command {
	return &cli.Command{
		Name:  "consistency",
		Usage: "check backup consistency",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "basepath",
				Aliases:  []string{"p"},
				Usage:    "the basepath of the decomposedfs (e.g. /var/tmp/opencloud/storage/users)",
				Required: true,
			},
			&cli.StringFlag{
				Name:    "blobstore",
				Aliases: []string{"b"},
				Usage:   "the blobstore type. Can be (none, decomposed, decomposeds3). Default decomposed",
				Value:   "decomposed",
			},
			&cli.BoolFlag{
				Name:  "fail",
				Usage: "exit with non-zero status if consistency check fails",
			},
		},
		Action: func(c *cli.Context) error {
			basePath := c.String("basepath")
			if basePath == "" {
				fmt.Println("basepath is required")
				return cli.ShowCommandHelp(c, "consistency")
			}

			var (
				bs  backup.ListBlobstore
				err error
			)
			switch c.String("blobstore") {
			case "decomposeds3":
				bs, err = decomposeds3bs.New(
					cfg.StorageUsers.Drivers.DecomposedS3.Endpoint,
					cfg.StorageUsers.Drivers.DecomposedS3.Region,
					cfg.StorageUsers.Drivers.DecomposedS3.Bucket,
					cfg.StorageUsers.Drivers.DecomposedS3.AccessKey,
					cfg.StorageUsers.Drivers.DecomposedS3.SecretKey,
					decomposeds3bs.Options{},
				)
			case "decomposed":
				bs, err = decomposedbs.New(basePath)
			case "none":
				bs = nil
			default:
				err = errors.New("blobstore type not supported")
			}
			if err != nil {
				fmt.Println(err)
				return err
			}
			if err := backup.CheckProviderConsistency(basePath, bs, c.Bool("fail")); err != nil {
				fmt.Println(err)
				return err
			}

			return nil
		},
	}
}

func init() {
	register.AddCommand(BackupCommand)
}
