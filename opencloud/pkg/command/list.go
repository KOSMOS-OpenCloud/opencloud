package command

import (
	"fmt"
	"log"
	"net"
	"net/rpc"

	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/pkg/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ListCommand is the entrypoint for the list command.
func ListCommand(cfg *config.Config) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "list OpenCloud services running in the runtime (supervised mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := rpc.DialHTTP("tcp", net.JoinHostPort(cfg.Runtime.Host, cfg.Runtime.Port))
			if err != nil {
				log.Fatalf("Failed to connect to the runtime. Has the runtime been started and did you configure the right runtime address (\"%s\")", cfg.Runtime.Host+":"+cfg.Runtime.Port)
			}

			var arg1 string

			if err := client.Call("Service.List", struct{}{}, &arg1); err != nil {
				log.Fatal(err)
			}

			fmt.Println(arg1)

			return nil
		},
	}
	listCmd.Flags().String("hostname", "localhost", "hostname of the runtime")
	viper.BindEnv("hostname", "OC_RUNTIME_HOST")
	viper.BindPFlag("hostname", listCmd.Flags().Lookup("hostname"))

	listCmd.Flags().String("port", "9250", "port of the runtime")
	viper.BindEnv("port", "OC_RUNTIME_PORT")
	viper.BindPFlag("port", listCmd.Flags().Lookup("port"))
	return listCmd
}

func init() {
	register.AddCommand(ListCommand)
}
