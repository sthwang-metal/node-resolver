// Package cmd is our cobra/viper cli implementation
package cmd

import (
	"fmt"
	"os"
	"strings"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.infratographer.com/x/crdbx"
	"go.infratographer.com/x/loggingx"
	"go.infratographer.com/x/otelx"
	"go.infratographer.com/x/versionx"
	"go.infratographer.com/x/viperx"
	"go.uber.org/zap"

	"go.infratographer.com/node-resolver/internal/config"
)

const appName = "node-resolver"

var (
	cfgFile       string
	logger        *zap.SugaredLogger
	defaultSchema string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   appName,
	Short: "A utility for resolving graphql nodes",
	Long:  `node-resolver is a service for resolving any node id in the graph to a type`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(defSchema string) {
	defaultSchema = defSchema
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/."+appName+".yaml)")
	viperx.MustBindFlag(viper.GetViper(), "config", rootCmd.PersistentFlags().Lookup("config"))

	// Logging flags
	loggingx.MustViperFlags(viper.GetViper(), rootCmd.PersistentFlags())

	// Register version command
	versionx.RegisterCobraCommand(rootCmd, func() { versionx.PrintVersion(logger) })
	otelx.MustViperFlags(viper.GetViper(), rootCmd.Flags())
	crdbx.MustViperFlags(viper.GetViper(), rootCmd.Flags())
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		cobra.CheckErr(err)

		// load the config file
		viper.AddConfigPath(home)
		viper.SetConfigName("." + appName)
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.SetEnvPrefix("noderesolver")

	viper.AutomaticEnv() // read in environment variables that match

	setupAppConfig()

	// setupLogging()
	logger = loggingx.InitLogger(appName, config.AppConfig.Logging)

	// If a config file is found, read it in.
	err := viper.ReadInConfig()
	if err == nil {
		logger.Infow("using config file",
			"file", viper.ConfigFileUsed(),
		)
	}
}

// setupAppConfig loads our config.AppConfig struct with the values bound by
// viper. Then, anywhere we need these values, we can just return to AppConfig
// instead of performing viper.GetString(...), viper.GetBool(...), etc.
func setupAppConfig() {
	err := viper.Unmarshal(&config.AppConfig)
	if err != nil {
		fmt.Printf("unable to decode app config: %s", err)
		os.Exit(1)
	}
}
