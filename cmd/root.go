package cmd

import (
	"os"

	"github.com/spf13/viper"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "image-bed",
	Short: "A simple image hosting application",
	Run: func(cmd *cobra.Command, args []string) {
		serveCmd.Run(cmd, args)
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "config file path (eg: /etc/image-bed/config.yaml)")
	err := viper.BindPFlag("config_file_path", rootCmd.PersistentFlags().Lookup("config"))
	if err != nil {
		return
	}
}
