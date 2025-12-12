package command

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	ocinit "github.com/opencloud-eu/opencloud/opencloud/pkg/init"
	"github.com/opencloud-eu/opencloud/opencloud/pkg/register"
	"github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/opencloud/pkg/config/defaults"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// InitCommand is the entrypoint for the init command
func InitCommand(_ *config.Config) *cobra.Command {
	initCmd := &cobra.Command{
		Use:     "init",
		Short:   "initialise an OpenCloud config",
		GroupID: CommandGroupServer,
		RunE: func(cmd *cobra.Command, args []string) error {
			insecureFlag := cmd.Flag("insecure").Value.String()
			insecure := false
			if insecureFlag == "ask" {
				answer := strings.ToLower(stringPrompt("Do you want to configure OpenCloud with certificate checking disabled?\n This is not recommended for public instances! [yes | no = default]"))
				if answer == "yes" || answer == "y" {
					insecure = true
				}
			} else if insecureFlag == strings.ToLower("true") || insecureFlag == strings.ToLower("yes") || insecureFlag == strings.ToLower("y") {
				insecure = true
			}
			forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
			diff, _ := cmd.Flags().GetBool("force-overwrite")
			err := ocinit.CreateConfig(insecure, forceOverwrite,
				diff, cmd.Flag("config-path").Value.String(),
				cmd.Flag("admin-password").Value.String())
			if err != nil {
				log.Fatalf("Could not create config: %s", err)
			}
			return nil
		},
	}
	initCmd.Flags().String("insecure", "ask", "Allow insecure OpenCloud config")
	err := viper.BindEnv("insecure", "OC_INSECURE")
	if err != nil {
		log.Fatalf("Could not bind environment variable OC_INSECURE: %s", err)
	}
	err = viper.BindPFlag("insecure", initCmd.Flags().Lookup("insecure"))
	if err != nil {
		log.Fatalf("Could not bind flag OC_INSECURE: %s", err)
	}

	initCmd.Flags().BoolP("diff", "d", false, "Show the difference between the current config and the new one")

	initCmd.Flags().BoolP("force-overwrite", "f", false, "Force overwrite existing config file")
	err = viper.BindEnv("force-overwrite", "OC_FORCE_CONFIG_OVERWRITE")
	if err != nil {
		log.Fatalf("Could not bind environment variable OC_FORCE_CONFIG_OVERWRITE: %s", err)
	}
	err = viper.BindPFlag("force-overwrite", initCmd.Flags().Lookup("force-overwrite"))
	if err != nil {
		log.Fatalf("Could not bind flag OC_FORCE_CONFIG_OVERWRITE: %s", err)
	}

	initCmd.Flags().String("config-path", defaults.BaseConfigPath(), "Config path for the OpenCloud runtime")
	err = viper.BindEnv("config-path", "OC_CONFIG_DIR")
	if err != nil {
		log.Fatalf("Could not bind environment variable OC_CONFIG_DIR: %s", err)
	}
	err = viper.BindEnv("config-path", "OC_BASE_DATA_PATH")
	if err != nil {
		log.Fatalf("Could not bind environment variable OC_BASE_DATA_PATH: %s", err)
	}
	err = viper.BindPFlag("config-path", initCmd.Flags().Lookup("config-path"))
	if err != nil {
		log.Fatalf("Could not bind flag OC_BASE_DATA_PATH: %s", err)
	}

	initCmd.Flags().String("admin-password", "", "Set admin password instead of using a random generated one")
	err = viper.BindEnv("admin-password", "ADMIN_PASSWORD")
	if err != nil {
		log.Fatalf("Could not bind environment variable ADMIN_PASSWORD: %s", err)
	}
	err = viper.BindEnv("admin-password", "IDM_ADMIN_PASSWORD")
	if err != nil {
		log.Fatalf("Could not bind environment variable IDM_ADMIN_PASSWORD: %s", err)
	}
	err = viper.BindPFlag("admin-password", initCmd.Flags().Lookup("admin-password"))
	if err != nil {
		log.Fatalf("Could not bind flag IDM_ADMIN_PASSWORD: %s", err)
	}
	return initCmd
}

func init() {
	register.AddCommand(InitCommand)
}

func stringPrompt(label string) string {
	input := ""
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, label+" ")
		input, _ = reader.ReadString('\n')
		if input != "" {
			break
		}
	}
	return strings.TrimSpace(input)
}
