package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
	"zcosmos"

	wasmd_x_wasm_types "github.com/CosmWasm/wasmd/x/wasm/types"
	juno_v11_app "github.com/CosmosContracts/juno/v11/app"
	sdk_types "github.com/cosmos/cosmos-sdk/types"
)

func init() {
	cfg := sdk_types.GetConfig()
	cfg.SetBech32PrefixForAccount(juno_v11_app.Bech32PrefixAccAddr, juno_v11_app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(juno_v11_app.Bech32PrefixValAddr, juno_v11_app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(juno_v11_app.Bech32PrefixConsAddr, juno_v11_app.Bech32PrefixConsPub)
	cfg.SetAddressVerifier(wasmd_x_wasm_types.VerifyAddressLen())
	cfg.Seal()
}

var (
	encodingConfig = juno_v11_app.MakeEncodingConfig()
	networkConfig  = zcosmos.NetworkConfig{
		Network:             "juno",
		Bech32PrefixAccAddr: juno_v11_app.Bech32PrefixAccAddr,
		StallThreshold:      5 * time.Minute,
		Codec:               encodingConfig.Marshaler,
		TxConfig:            encodingConfig.TxConfig,
	}
)

func main() {
	err := zcosmos.RunMain(context.Background(), zcosmos.RunConfig{
		Program:   "zenith-juno",
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Args:      os.Args[1:],
		APIAddr:   ":4415",
		DebugAddr: ":4416",

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
