package cmd

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.infratographer.com/x/echox"
	"go.infratographer.com/x/otelx"
	"go.infratographer.com/x/versionx"
	"go.infratographer.com/x/viperx"
	"go.uber.org/zap"

	"go.infratographer.com/node-resolver/internal/config"
	"go.infratographer.com/node-resolver/internal/graphapi"
)

var (
	defaultListenAddr = ":7904"
	schemaFile        = ""
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the location API",
	Run: func(cmd *cobra.Command, args []string) {
		serve(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	echox.MustViperFlags(viper.GetViper(), serveCmd.Flags(), defaultListenAddr)

	serveCmd.Flags().StringVar(&schemaFile, "schema", "", "path to graphql schema file")
	viperx.MustBindFlag(viper.GetViper(), "schema", serveCmd.Flags().Lookup("schema"))
}

func serve(ctx context.Context) {
	err := otelx.InitTracer(config.AppConfig.Tracing, appName, logger)
	if err != nil {
		logger.Fatalw("failed to initialize tracer", "error", err)
	}

	srv, err := echox.NewServer(
		logger.Desugar(),
		echox.Config{
			Listen:              viper.GetString("server.listen"),
			ShutdownGracePeriod: viper.GetDuration("server.shutdown-grace-period"),
		},
		versionx.BuildDetails(),
	)
	if err != nil {
		logger.Fatalw("failed to create server", zap.Error(err))
	}

	schema := defaultSchema
	if schemaFile == "" {
		logger.Warn("no schema file provided, starting with default schema")
	} else {
		schemaBytes, err := os.ReadFile(schemaFile)
		if err != nil {
			logger.Fatalw("failed to read graphql schema file", "error", err)
		}

		schema = string(schemaBytes)
	}

	r, err := graphapi.NewResolver(logger.Named("resolvers"), schema)
	if err != nil {
		logger.Fatalw("failed to create graphql resolver", "error", err)
	}

	srv.AddHandler(r)

	if err := srv.RunWithContext(ctx); err != nil {
		logger.Errorw("failed to run server", "error", zap.Error(err))
	}
}
