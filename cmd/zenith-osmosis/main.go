package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
	"zcosmos"

	osmosis_v12_app "github.com/osmosis-labs/osmosis/v12/app"
	osmosis_v12_app_params "github.com/osmosis-labs/osmosis/v12/app/params"
)

var (
	encodingConfig = osmosis_v12_app.MakeEncodingConfig()
	networkConfig  = zcosmos.NetworkConfig{
		Network:             "osmosis",
		Bech32PrefixAccAddr: osmosis_v12_app_params.Bech32PrefixAccAddr,
		StallThreshold:      5 * time.Minute,
		Codec:               encodingConfig.Marshaler,
		TxConfig:            encodingConfig.TxConfig,
	}
)

func main() {
	err := zcosmos.RunMain(context.Background(), zcosmos.RunConfig{
		Program:   "zenith-osmosis",
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Args:      os.Args[1:],
		APIAddr:   ":4413",
		DebugAddr: ":4414",

		NetworkConfig: networkConfig,
	})
	switch {
	case err == nil:
		os.Exit(0)
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case zcosmos.IsSignalError(err):
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
