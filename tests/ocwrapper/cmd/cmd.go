package cmd

import (
	"fmt"

	"ocwrapper/common"
	opencloud "ocwrapper/opencloud"
	opencloudConfig "ocwrapper/opencloud/config"
	wrapper "ocwrapper/wrapper"
	wrapperConfig "ocwrapper/wrapper/config"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ocwrapper",
	Short: "ocwrapper is a wrapper for opencloud server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.Help(); err != nil {
			fmt.Printf("error executing help command: %v\n", err)
		}
	},
}

func serveCmd() *cobra.Command {
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Starts the server",
		Run: func(cmd *cobra.Command, args []string) {
			common.Wg.Add(2)

			// set configs
			opencloudConfig.Set("bin", cmd.Flag("bin").Value.String())
			opencloudConfig.Set("url", cmd.Flag("url").Value.String())
			opencloudConfig.Set("retry", cmd.Flag("retry").Value.String())
			opencloudConfig.Set("adminUsername", cmd.Flag("admin-username").Value.String())
			opencloudConfig.Set("adminPassword", cmd.Flag("admin-password").Value.String())

			if cmd.Flag("skip-OpenCloud-run").Value.String() == "false" {
				go opencloud.Start(nil)
			}
			go wrapper.Start(cmd.Flag("port").Value.String())
		},
	}

	// serve command args
	serveCmd.Flags().SortFlags = false
	serveCmd.Flags().StringP("bin", "", opencloudConfig.Get("bin"), "Full opencloud binary path")
	serveCmd.Flags().StringP("url", "", opencloudConfig.Get("url"), "opencloud server url")
	serveCmd.Flags().StringP("retry", "", opencloudConfig.Get("retry"), "Number of retries to start opencloud server")
	serveCmd.Flags().StringP("port", "p", wrapperConfig.Get("port"), "Wrapper API server port")
	serveCmd.Flags().StringP("admin-username", "", "", "admin username for opencloud server")
	serveCmd.Flags().StringP("admin-password", "", "", "admin password for opencloud server")
	serveCmd.Flags().Bool("skip-OpenCloud-run", false, "Skip running opencloud server")

	return serveCmd
}

// Execute executes the command
func Execute() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(serveCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("error executing command: %v\n", err)
	}
}
