package main

import (
	"github.com/cripito/amiproxy/logs"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version = "0.01"

func main() {
	logs.Init()

	cobra.OnInitialize(readConfig)

	p := RootCmd.PersistentFlags()

	p.BoolP("version", "V", false, "Print version information and exit")

	p.StringVar(&cfgFile, "config", "ami-proxy.yaml", "config file (default is $HOME/.ami-proxy.yaml)")
	verbose = p.BoolP("verbose", "v", false, "Enable verbose logging")

	p.String("messagebus.url", nats.DefaultURL, "URL for connecting to the Message Bus cluster")
	p.String("ami.username", "", "Username for connecting to AMI")
	p.String("ami.password", "", "Password for connecting to AMI")
	p.String("ami.port", "5038", "Port to connecting to the AMI")
	p.String("ami.host", "localhost", "Host to connecting to the AMI")
	p.String("ami.events", "on", "Track events")

	for _, n := range []string{"verbose", "messagebus.url", "ami.events", "ami.username", "ami.password", "ami.port", "ami.host"} {
		err := viper.BindPFlag(n, p.Lookup(n))
		if err != nil {
			panic("failed to bind flag " + n)
		}
	}

	if err := RootCmd.Execute(); err != nil {
		return
	}
}
