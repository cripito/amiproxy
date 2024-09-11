package main

import (
	"context"
	"fmt"
	"os"

	amilib "github.com/cripito/amilib"
	"github.com/cripito/amiproxy/logs"
	"github.com/cripito/amiproxy/proxy"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose *bool
)

// RootCmd is the Cobra root command descriptor
var RootCmd = &cobra.Command{
	Use:   "ami-proxy",
	Short: "Proxy for the Asterisk AMI interface.",
	Long: `ami-proxy is a proxy for working the Asterisk Manager Interface over NATS.
	AMI commands are exposed over NATS for operation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if ok, _ := cmd.PersistentFlags().GetBool("version"); ok { // nolint: gas
			fmt.Println(version)
			os.Exit(0)
		}

		if viper.GetBool("verbose") {
			logs.TLogger.Info().Msg("verbose logging enabled")

		} else {

		}

		//native.Logger.SetHandler(handler)

		return runServer(ctx)
	},
}

func Init() {

}

// readConfig reads in config file and ENV variables if set.
func readConfig() {
	viper.SetConfigName("ami-proxy") // name of config file (without extension)
	//viper.AddConfigPath("$HOME")     // adding home directory as first search path
	viper.AddConfigPath(".") // adding actual directory as first search path

	if cfgFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfgFile)
	}

	// Load from the environment, as well
	//viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	//viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	err := viper.ReadInConfig()
	if err == nil {
		logs.TLogger.Debug().Msg("read configuration from file")

		return
	}
}

func runServer(ctx context.Context) error {
	messagebusURL := viper.GetString("messagebus.url")

	if *verbose {
		logs.TLogger.Debug().Msgf("Message bus %s", messagebusURL)
	}

	logs.TLogger.Info().Msgf("starting ami-proxy server %s - %s", version, messagebusURL)

	cfg := amilib.ConfigData{}
	cfg.NatsUrl = messagebusURL
	cfg.UserName = viper.GetString("ami.username")
	cfg.Password = viper.GetString("ami.password")
	cfg.Port = viper.GetInt("ami.port")
	cfg.Verbose = *verbose
	cfg.Server = viper.GetString("ami.host")

	cfg.Events = false
	if viper.GetString("ami.events") == "on" {
		cfg.Events = true
	}

	logs.TLogger.Info().Msgf("cfg options %+v", cfg)

	srv := proxy.New(cfg)
	defer srv.Close()

	logs.TLogger.Info().Msgf("Config: %+v", srv)
	srv.SetLogHandler(logs.TLogger)

	logs.TLogger.Info().Msgf("starting ami-proxy server %s - %s", version, messagebusURL)
	return srv.Listen(ctx)
}
