package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/jinzhu/configor"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	flag "github.com/spf13/pflag"
	"gitlab.com/thorchain/bepswap/statechain/cmd"

	"gitlab.com/thorchain/bepswap/observe/config"
	"gitlab.com/thorchain/bepswap/observe/x/observer"
)

// we define version / revision here , so we could inject the version from CI pipeline if we want to
var (
	version  string
	revision string
)

const (
	serverIdentity = "observer"
)

func printVersion() {
	fmt.Printf("%s v%s, rev %s\n", serverIdentity, version, revision)
}

func main() {
	showVersion := flag.Bool("version", false, "Shows version")
	// TODO set the default log level to info later
	logLevel := flag.StringP("log-level", "l", "debug", "Log Level")
	pretty := flag.BoolP("pretty-log", "p", false, "Enables unstructured prettified logging. This is useful for local debugging")
	cfgFile := flag.StringP("cfg", "c", "config.json", "configuration file path name")
	flag.Parse()
	if *showVersion {
		printVersion()
		return
	}
	cosmosSDKConfg := sdk.GetConfig()
	cosmosSDKConfg.SetBech32PrefixForAccount(cmd.Bech32PrefixAccAddr, cmd.Bech32PrefixAccPub)
	cosmosSDKConfg.Seal()
	initLog(*logLevel, *pretty)
	var cfg config.Configuration
	if err := configor.Load(&cfg, *cfgFile); nil != err {
		log.Fatal().Err(err).Msg("fail to read from config file")
	}
	obs, err := observer.NewObserver(cfg)
	if nil != err {
		log.Fatal().Err(err).Msg("fail to create observer")
	}
	if err := obs.Start(); nil != err {
		log.Fatal().Err(err).Msg("fail to start observer")
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Info().Msg("stop signal received")
	if err := obs.Stop(); nil != err {
		log.Fatal().Err(err).Msg("fail to stop observer")
	}

}

func initLog(level string, pretty bool) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		log.Warn().Msgf("%s is not a valid log-level, falling back to 'info'", level)
	}
	var out io.Writer = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	zerolog.SetGlobalLevel(l)
	log.Logger = log.Output(out).With().Str("service", serverIdentity).Logger()
}
