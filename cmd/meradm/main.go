package main

import (
	"time"

	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "meradm",
	Short: "admin tool for merlin - distributed IPVS manager",
}

var (
	debug   bool
	host    string
	port    uint16
	timeout time.Duration
)

func init() {
	cobra.OnInitialize(initLogs)
	f := rootCmd.PersistentFlags()
	f.BoolVarP(&debug, "debug", "X", false, "enable debug logging")
	f.StringVarP(&host, "host", "H", "localhost", "merlin host to connect to")
	f.Uint16VarP(&port, "port", "P", 4282, "merlin port to connect to")
	f.DurationVar(&timeout, "timeout", 10*time.Second, "client timeout")
}

func initLogs() {
	if debug {
		log.SetLevel(log.DebugLevel)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
